// Copyright 2021 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package raftlease

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/juju/clock"
	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"github.com/juju/pubsub/v2"
	"github.com/juju/retry"
	"github.com/juju/worker/v2"
	"github.com/juju/worker/v2/catacomb"
	"gopkg.in/tomb.v2"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/base"
	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/core/lease"
	"github.com/juju/juju/core/raftlease"
	"github.com/juju/juju/pubsub/apiserver"
)

// Logger is a in place interface to represent a logger for consuming.
type Logger interface {
	Errorf(string, ...interface{})
	Debugf(string, ...interface{})
}

// Remote defines an interface for managing remote connections for the client.
type Remote interface {
	worker.Worker
	Address() string
	SetAddress(string)
	Request(context.Context, *raftlease.Command) error
}

// ClientMetrics represents the metrics during a client request.
type ClientMetrics interface {
	RecordOperation(string, string, time.Time)
}

type Config struct {
	APIInfo        *api.Info
	Hub            *pubsub.StructuredHub
	ForwardTimeout time.Duration
	NewRemote      func(RemoteConfig) Remote
	ClientMetrics  ClientMetrics
	Clock          clock.Clock
	Logger         Logger
	Random         *rand.Rand
}

// Validate validates the raft lease worker configuration.
func (config Config) Validate() error {
	if config.Hub == nil {
		return errors.NotValidf("nil Hub")
	}
	if config.APIInfo == nil {
		return errors.NotValidf("nil APIInfo")
	}
	if config.NewRemote == nil {
		return errors.NotValidf("nil NewRemote")
	}
	if config.Logger == nil {
		return errors.NotValidf("nil Logger")
	}
	if config.ClientMetrics == nil {
		return errors.NotValidf("nil ClientMetrics")
	}
	if config.Random == nil {
		return errors.NotValidf("nil Random")
	}
	if config.Clock == nil {
		return errors.NotValidf("nil Clock")
	}
	return nil
}

type Client struct {
	config        Config
	catacomb      catacomb.Catacomb
	serverDetails chan apiserver.Details

	mutex           sync.Mutex
	servers         map[string]Remote
	lastKnownRemote Remote
}

// NewClient creates a new client for connecting to remote controllers.
func NewClient(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	client := &Client{
		config:        config,
		serverDetails: make(chan apiserver.Details),
		servers:       make(map[string]Remote),
	}

	// Subscribe to API server address changes.
	unsubscribe, err := config.Hub.Subscribe(
		apiserver.DetailsTopic,
		client.apiserverDetailsChanged,
	)
	if err != nil {
		return nil, errors.Annotate(err, "subscribing to apiserver details")
	}
	// Now that we're subscribed, request the current API server details.
	// This will be used to convert the localhost address from the api.Info
	// to a ip address (10.x.x.x as an example).
	req := apiserver.DetailsRequest{
		Requester: "raft-lease-client",
		LocalOnly: true,
	}
	if _, err := config.Hub.Publish(apiserver.DetailsRequestTopic, req); err != nil {
		unsubscribe()
		return nil, errors.Annotate(err, "requesting current apiserver details")
	}

	if err := catacomb.Invoke(catacomb.Plan{
		Site: &client.catacomb,
		Work: func() error {
			defer unsubscribe()
			return client.loop()
		},
	}); err != nil {
		unsubscribe()
		return nil, errors.Trace(err)
	}

	// Wait for at least one server connection.
	if err := client.initServers(); err != nil {
		unsubscribe()
		return nil, errors.Trace(err)
	}

	// Add all the remote servers to the catacomb.
	for _, remote := range client.servers {
		if err := client.catacomb.Add(remote); err != nil {
			unsubscribe()
			return nil, errors.Trace(err)
		}
	}

	return client, nil
}

// Request attempts to perform a raft lease command against the leader.
func (c *Client) Request(ctx context.Context, command *raftlease.Command) error {
	start := c.config.Clock.Now()
	timeout := c.config.Clock.After(c.config.ForwardTimeout)

	remote, err := c.selectRemote()
	if err != nil {
		// TODO (stickupkid): If we find no remotes, should we force an attempt
		// of a connection?
		return errors.Trace(err)
	}

	// Attempt to request at least 3 times. This isn't a retry of the request
	// against the same api controller. Instead this is should attempt to find
	// a new api controller to hit.
	err = retry.Call(retry.CallArgs{
		Func: func() error {
			select {
			case <-ctx.Done():
				c.record(command.Operation, "delivery timeout", start)
				return lease.ErrTimeout
			case <-timeout:
				return lease.ErrTimeout
			default:
			}

			err := remote.Request(ctx, command)

			// If the error is nil, we've done it successfully.
			if err == nil {
				// We had a successful connection against that remote, set it to
				// the lastKnownRemote.
				c.mutex.Lock()
				c.lastKnownRemote = remote
				c.mutex.Unlock()

				c.record(command.Operation, "success", start)
				return nil
			}

			// If the remote is no longer the leader, go and attempt to get it from
			// the error. If it's not in the error, just select one at random.
			if apiservererrors.IsNotLeaderError(err) {
				// Grab the underlying not leader error.
				notLeaderError := errors.Cause(err).(*apiservererrors.NotLeaderError)

				remote, err = c.selectRemoteFromError(remote.Address(), err)
				if err == nil && remote != nil {
					// If we've got an remote, then attempt the request again.
					return errors.Annotatef(notLeaderError, "not the leader, trying again")
				}
				// If we're not the leader and we don't have a remote to select from
				// just return back.
				if notLeaderError.ServerAddress() == "" {
					// The raft instance isn't clustered, we don't have a way
					// forward, so send back a dropped error.
					c.config.Logger.Errorf("No leader found and no cluster available, dropping command: %v", command)
					return lease.ErrDropped
				}
			}

			return errors.Trace(err)
		},
		IsFatalError: func(err error) bool {
			return lease.IsDropped(err)
		},
		Attempts:    3,
		Delay:       time.Millisecond * 100,
		MaxDelay:    time.Second * 5,
		BackoffFunc: retry.DoubleDelay,
		Stop:        ctx.Done(),
		Clock:       c.config.Clock,
	})

	// If the retry has stopped, then we've been cancelled, so we need to tell
	// the lease manager that we've timedout.
	if retry.IsRetryStopped(err) {
		err = lease.ErrTimeout
	}

	c.record(command.Operation, "error", start)
	return errors.Trace(err)
}

// Close closes the client.
func (c *Client) Close() error {
	c.catacomb.Kill(nil)
	return c.catacomb.Wait()
}

func (c *Client) record(operation, result string, start time.Time) {
	c.config.ClientMetrics.RecordOperation(operation, result, start)
}

// Attempt to use the last known remote, if that's not around, then just select
// the first one available. If nothing is around, then return an error.
func (c *Client) selectRemote() (Remote, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.lastKnownRemote != nil {
		return c.lastKnownRemote, nil
	}

	for _, remote := range c.servers {
		return remote, nil
	}

	return nil, errors.NotFoundf("remote servers")
}

func (c *Client) selectRemoteFromError(addr string, err error) (Remote, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Throw away the last known remote, as we can't use that reliably.
	c.lastKnownRemote = nil

	// Use the not leader error to locate the server ID from our list of
	// servers.
	leaderErr := err.(*apiservererrors.NotLeaderError)
	if remote, ok := c.servers[leaderErr.ServerID()]; ok {
		// Ignore the remote address and address check here, it might have
		// switched over during the request. As this is more of authority on
		// this, just return back the remote.
		return remote, nil
	}

	// Fallback to just attempting to get one of the servers.
	sorted := make([]Remote, 0, len(c.servers))
	for _, remote := range c.servers {
		if remote.Address() == addr {
			continue
		}

		sorted = append(sorted, remote)
	}

	// Unlike the not leader error, we don't have an authority here. So
	// attempt to locate a new remote that isn't the one we just tried.
	if num := len(sorted); num > 0 {
		return sorted[c.config.Random.Intn(num)], nil
	}

	return nil, errors.NotFoundf("no leader found: remote server connection")
}

func (c *Client) apiserverDetailsChanged(topic string, details apiserver.Details, err error) {
	if err != nil {
		// This should never happen, so treat it as fatal.
		c.catacomb.Kill(errors.Annotate(err, "apiserver details callback failed"))
		return
	}
	select {
	case <-c.catacomb.Dying():
	case c.serverDetails <- details:
	}
}

func (c *Client) loop() error {
	for {
		select {
		case <-c.catacomb.Dying():
			return c.catacomb.ErrDying()
		case details := <-c.serverDetails:
			// Get the primary address for each server ID.
			addresses := c.gatherAddresses(details)
			if len(addresses) == 0 {
				// If there are no addresses, then nothing is routable. In this
				// case, we'll continue to use the current addresses.
				c.config.Logger.Errorf("no server addresses found, will continue to use old addresses")
				continue
			}

			if err := c.ensureServers(addresses); err != nil {
				return errors.Trace(err)
			}
		}
	}
}

func (c *Client) initServers() error {
	if len(c.config.APIInfo.Addrs) == 0 {
		return errors.NotFoundf("api addresses")
	}

	for k, address := range c.config.APIInfo.Addrs {
		info := *c.config.APIInfo
		info.Addrs = []string{address}

		remote := c.config.NewRemote(RemoteConfig{
			APIInfo: &info,
			Clock:   c.config.Clock,
			Logger:  c.config.Logger,
		})

		// In reality it doesn't matter what these get called, as a later down
		// the line they'll be replaced via the request from the
		// apiserver.Details result.
		key := fmt.Sprintf("%d", k)
		c.servers[key] = remote
	}

	return nil
}

// gatherAddresses turns a series of data addresses into a map of server ids
// and internal address. If no internal address is found, then fallback to
// the external sorted addresses.
func (c *Client) gatherAddresses(details apiserver.Details) map[string]string {
	if len(details.Servers) == 0 {
		return nil
	}

	servers := make(map[string]string)
	for id, server := range details.Servers {
		var address string
		if server.InternalAddress != "" {
			address = server.InternalAddress
		} else if len(server.Addresses) > 0 {
			// The sorting of the addresses is done during the publishing of
			// the event, so we can depend on the correct ordering.
			address = server.Addresses[0]
		}

		// We can't use this server, so skip it.
		if address == "" {
			continue
		}
		servers[id] = address
	}
	return servers
}

func (c *Client) ensureServers(addresses map[string]string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	witnessed := set.NewStrings()
	for id, address := range addresses {
		witnessed.Add(id)

		// If we already have a server, don't tear it down, just update the
		// address.
		if server, found := c.servers[id]; found {
			server.SetAddress(address)
		} else {
			info := *c.config.APIInfo
			info.Addrs = []string{address}

			remote := c.config.NewRemote(RemoteConfig{
				APIInfo: &info,
				Clock:   c.config.Clock,
				Logger:  c.config.Logger,
			})
			c.servers[id] = remote
			if err := c.catacomb.Add(remote); err != nil {
				return errors.Trace(err)
			}
		}
	}

	// Kill off any servers that have fallen away. In a highly available setup,
	// this might actually be really painful for us. We should really consider
	// if this is wise or not? During flapping instances, we take a long time
	// to connect and this could cause stalling of message sending.
	//
	// Instead we should probably just tombstone them first time around, then
	// next time around we should reap them.
	for id, remote := range c.servers {
		if witnessed.Contains(id) {
			continue
		}

		remote.Kill()

		if err := remote.Wait(); err != nil {
			// We don't care in reality about the death rattle of a server, as
			// it's already dead to us.
			c.config.Logger.Errorf("error waiting for remote server death: %v", err)
		}
		// Ensure we still delete the id from the server list, even though the
		// remote Wait might have failed.
		delete(c.servers, id)
	}
	return nil
}

// RemoteConfig defines the configuration for creating a NewRemote.
type RemoteConfig struct {
	APIInfo *api.Info
	Clock   clock.Clock
	Logger  Logger
}

// NewRemote creates a new Remote from a given address.
func NewRemote(config RemoteConfig) Remote {
	r := &remote{
		config: config,
	}
	r.tomb.Go(r.loop)
	return r
}

// RaftLeaseApplier defines a client for applying leases.
type RaftLeaseApplier interface {
	ApplyLease(command string) error
}

type remote struct {
	config         RemoteConfig
	mutex          sync.Mutex
	tomb           tomb.Tomb
	stopConnecting chan struct{}

	api    base.APICallCloser
	client RaftLeaseApplier
}

// Address returns the current remote server address.
func (r *remote) Address() string {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.config.APIInfo.Addrs) == 0 {
		return ""
	}
	return r.config.APIInfo.Addrs[0]
}

// SetAddress updates the current remote server address. This will cause
// the closing of the underlying connection.
func (r *remote) SetAddress(addr string) {
	// They're the same address, nothing to do here.
	if r.Address() == addr {
		return
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.api == nil && r.stopConnecting != nil {
		close(r.stopConnecting)
		r.stopConnecting = nil
	}
	r.config.APIInfo.Addrs = []string{addr}
}

// Request performs a request against a specific api.
func (r *remote) Request(ctx context.Context, command *raftlease.Command) error {
	if r.client == nil {
		r.config.Logger.Errorf("No attached client instance; dropping command: %v", command)
		return lease.ErrDropped
	}

	bytes, err := command.Marshal()
	if err != nil {
		return errors.Trace(err)
	}

	// Check that the context hasn't been canceled before applying the lease.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return r.client.ApplyLease(string(bytes))
}

// Kill is part of the worker.Worker interface.
func (r *remote) Kill() {
	r.mutex.Lock()
	if r.api != nil {
		_ = r.api.Close()
	}
	r.mutex.Unlock()
	r.tomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (r *remote) Wait() error {
	return r.tomb.Wait()
}

func (r *remote) loop() error {
	var delay <-chan time.Time
	for {
		if r.api == nil {
			if r.connect() {
				delay = nil
			} else {
				// Apply a back-off if we where unable to connect.
				delay = r.config.Clock.After(time.Second * 5)
			}
		}

		select {
		case <-r.tomb.Dying():
			r.config.Logger.Debugf("remote shutting down")
			return tomb.ErrDying
		case <-delay:
			// If we failed to connect for whatever reason, this means we don't
			// cycle immediately.
		}
	}
}

func (r *remote) connect() bool {
	stop := make(chan struct{})

	var info *api.Info
	r.mutex.Lock()
	info = r.config.APIInfo
	r.stopConnecting = stop
	r.mutex.Unlock()

	address := r.Address()
	r.config.Logger.Debugf("connecting to %s", address)

	var apiCloser base.APICallCloser
	_ = retry.Call(retry.CallArgs{
		Func: func() error {
			r.config.Logger.Debugf("open api to %v", address)
			conn, err := api.Open(info, api.DialOpts{
				DialAddressInterval: 50 * time.Millisecond,
				Timeout:             10 * time.Minute,
				RetryDelay:          2 * time.Second,
			})
			if err != nil {
				r.config.Logger.Errorf("unable to open api for %v, %v", address, err)
				return errors.Trace(err)
			}
			apiCloser = conn
			return nil
		},
		Attempts:    retry.UnlimitedAttempts,
		Delay:       time.Second,
		MaxDelay:    5 * time.Minute,
		BackoffFunc: retry.DoubleDelay,
		Stop:        stop,
		Clock:       r.config.Clock,
	})

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.stopConnecting = nil

	if apiCloser != nil {
		r.api = apiCloser
		r.client = NewAPI(r.api)
		return true
	}

	return false
}
