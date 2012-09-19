package mstate_test

import (
	"fmt"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/charm"
	state "launchpad.net/juju-core/mstate"
	"strings"
	"time"
)

type RelationSuite struct {
	ConnSuite
	charm *state.Charm
}

var _ = Suite(&RelationSuite{})

func (s *RelationSuite) SetUpTest(c *C) {
	s.ConnSuite.SetUpTest(c)
	s.charm = s.AddTestingCharm(c, "dummy")
}

func (s *RelationSuite) TestRelationErrors(c *C) {
	req, err := s.State.AddService("req", s.charm)
	c.Assert(err, IsNil)
	reqep := state.RelationEndpoint{"req", "ifce", "bar", state.RoleRequirer, charm.ScopeGlobal}

	// Check we can't add a relation until both services exist.
	proep := state.RelationEndpoint{"pro", "ifce", "foo", state.RoleProvider, charm.ScopeGlobal}
	_, err = s.State.AddRelation(proep, reqep)
	c.Assert(err, ErrorMatches, `cannot add relation "pro:foo req:bar": .*`)
	assertNoRelations(c, req)
	pro, err := s.State.AddService("pro", s.charm)
	c.Assert(err, IsNil)

	// Check that interfaces have to match.
	proep2 := state.RelationEndpoint{"pro", "other", "foo", state.RoleProvider, charm.ScopeGlobal}
	_, err = s.State.AddRelation(proep2, reqep)
	c.Assert(err, ErrorMatches, `cannot add relation "pro:foo req:bar": endpoints do not relate`)
	assertNoRelations(c, pro)
	assertNoRelations(c, req)

	// Check a variety of surprising endpoint combinations.
	_, err = s.State.AddRelation(reqep)
	c.Assert(err, ErrorMatches, `cannot add relation "req:bar": single endpoint must be a peer relation`)
	assertNoRelations(c, req)

	peer, err := s.State.AddService("peer", s.charm)
	c.Assert(err, IsNil)
	peerep := state.RelationEndpoint{"peer", "ifce", "baz", state.RolePeer, charm.ScopeGlobal}
	_, err = s.State.AddRelation(peerep, reqep)
	c.Assert(err, ErrorMatches, `cannot add relation "peer:baz req:bar": endpoints do not relate`)
	assertNoRelations(c, peer)
	assertNoRelations(c, req)

	_, err = s.State.AddRelation(peerep, peerep)
	c.Assert(err, ErrorMatches, `cannot add relation "peer:baz peer:baz": endpoints do not relate`)
	assertNoRelations(c, peer)

	_, err = s.State.AddRelation()
	c.Assert(err, ErrorMatches, `cannot add relation "": cannot relate 0 endpoints`)
	_, err = s.State.AddRelation(proep, reqep, peerep)
	c.Assert(err, ErrorMatches, `cannot add relation "peer:baz pro:foo req:bar": cannot relate 3 endpoints`)

	_, err = s.State.Relation(peerep)
	c.Assert(err, ErrorMatches, `cannot get relation "peer:baz": .*`)
}

func (s *RelationSuite) TestProviderRequirerRelation(c *C) {
	req, err := s.State.AddService("req", s.charm)
	c.Assert(err, IsNil)
	pro, err := s.State.AddService("pro", s.charm)
	c.Assert(err, IsNil)
	assertNoRelations(c, req)
	assertNoRelations(c, pro)

	// Add a relation, and check we can only do so once.
	proep := state.RelationEndpoint{"pro", "ifce", "foo", state.RoleProvider, charm.ScopeGlobal}
	reqep := state.RelationEndpoint{"req", "ifce", "bar", state.RoleRequirer, charm.ScopeGlobal}
	rel, err := s.State.AddRelation(proep, reqep)
	c.Assert(err, IsNil)
	_, err = s.State.AddRelation(proep, reqep)
	c.Assert(err, ErrorMatches, `cannot add relation "pro:foo req:bar": .*`)

	testWhenDying(c, rel, noErr, noErr, func() error {
		assertOneRelation(c, pro, 0, proep, reqep)
		assertOneRelation(c, req, 0, reqep, proep)
		return nil
	})

	// Remove the relation, and check it can't be removed again.
	err = s.State.RemoveRelation(rel)
	c.Assert(err, IsNil)

	err = s.State.RemoveRelation(rel)
	c.Assert(err, ErrorMatches, `cannot remove relation "pro:foo req:bar": .*`)

	// Check that we can add it again if we want to; but this time,
	// give one of the endpoints container scope and check that both
	// resulting service relations get that scope.
	reqep.RelationScope = charm.ScopeContainer
	_, err = s.State.AddRelation(proep, reqep)
	c.Assert(err, IsNil)
	// After adding relation, make proep container-scoped as well, for
	// simplicity of testing.
	proep.RelationScope = charm.ScopeContainer
	assertOneRelation(c, req, 2, reqep, proep)
	assertOneRelation(c, pro, 2, proep, reqep)
}

func (s *RelationSuite) TestPeerRelation(c *C) {
	peer, err := s.State.AddService("peer", s.charm)
	c.Assert(err, IsNil)
	peerep := state.RelationEndpoint{"peer", "ifce", "baz", state.RolePeer, charm.ScopeGlobal}
	assertNoRelations(c, peer)

	// Add a relation, and check we can only do so once.
	rel, err := s.State.AddRelation(peerep)
	c.Assert(err, IsNil)
	_, err = s.State.AddRelation(peerep)
	c.Assert(err, ErrorMatches, `cannot add relation "peer:baz": .*`)
	assertOneRelation(c, peer, 0, peerep)

	// Remove the relation, and check it can't be removed again.
	err = rel.Die()
	c.Assert(err, IsNil)
	err = s.State.RemoveRelation(rel)
	c.Assert(err, IsNil)
	assertNoRelations(c, peer)
	err = s.State.RemoveRelation(rel)
	c.Assert(err, ErrorMatches, `cannot remove relation "peer:baz": .*`)
}

func (s *RelationSuite) TestRemoveServiceRemovesRelations(c *C) {
	peer, err := s.State.AddService("peer", s.charm)
	c.Assert(err, IsNil)
	peerep := state.RelationEndpoint{"peer", "ifce", "baz", state.RolePeer, charm.ScopeGlobal}
	_, err = s.State.AddRelation(peerep)
	c.Assert(err, IsNil)
	err = peer.Die()
	c.Assert(err, IsNil)
	err = s.State.RemoveService(peer)
	c.Assert(err, IsNil)
	_, err = s.State.Service("peer")
	c.Assert(err, ErrorMatches, `cannot get service "peer": not found`)
	_, err = s.State.Relation(peerep)
	c.Assert(err, ErrorMatches, `cannot get relation "peer:baz": not found`)
}

func (s *RelationSuite) TestLifecycle(c *C) {
	peer, err := s.State.AddService("peer", s.charm)
	c.Assert(err, IsNil)
	peerep := state.RelationEndpoint{"peer", "ifce", "baz", state.RolePeer, charm.ScopeGlobal}
	assertNoRelations(c, peer)

	rel, err := s.State.AddRelation(peerep)
	c.Assert(err, IsNil)
	life := rel.Life()
	c.Assert(life, Equals, state.Alive)

	// Check legal next state.
	err = rel.Kill()
	c.Assert(err, IsNil)
	life = rel.Life()
	c.Assert(life, Equals, state.Dying)

	// Check legal repeated state setting.
	err = rel.Kill()
	c.Assert(err, IsNil)
	life = rel.Life()
	c.Assert(life, Equals, state.Dying)

	// Check non-dead removal.
	c.Assert(func() { s.State.RemoveRelation(rel) }, PanicMatches, `relation .* is not dead`)

	// Check final state.
	err = rel.Die()
	c.Assert(err, IsNil)
	life = rel.Life()
	c.Assert(life, Equals, state.Dead)
}

func assertNoRelations(c *C, srv *state.Service) {
	rels, err := srv.Relations()
	c.Assert(err, IsNil)
	c.Assert(rels, HasLen, 0)
}

func assertOneRelation(c *C, srv *state.Service, relId int, endpoints ...state.RelationEndpoint) {
	rels, err := srv.Relations()
	c.Assert(err, IsNil)
	c.Assert(rels, HasLen, 1)
	rel := rels[0]
	c.Assert(rel.Id(), Equals, relId)
	name := srv.Name()
	expectEp := endpoints[0]
	ep, err := rel.Endpoint(name)
	c.Assert(err, IsNil)
	c.Assert(ep, DeepEquals, expectEp)
	if len(endpoints) == 2 {
		expectEp = endpoints[1]
	}
	eps, err := rel.RelatedEndpoints(name)
	c.Assert(err, IsNil)
	c.Assert(eps, DeepEquals, []state.RelationEndpoint{expectEp})
}

type RelationUnitSuite struct {
	ConnSuite
}

var _ = Suite(&RelationUnitSuite{})

func (s *RelationUnitSuite) TestPeerSettings(c *C) {
	pr := NewPeerRelation(c, &s.ConnSuite)
	rus := RUs{pr.ru0, pr.ru1}

	// Check missing settings cannot be read by any RU.
	for _, ru := range rus {
		_, err := ru.ReadSettings("peer/0")
		c.Assert(err, ErrorMatches, `cannot read settings for unit "peer/0" in relation "peer:name": not found`)
	}

	// Add settings for one RU.
	node, err := pr.ru0.Settings()
	c.Assert(err, IsNil)
	c.Assert(node.Map(), HasLen, 0)
	node.Set("meme", "socially-awkward-penguin")
	_, err = node.Write()
	c.Assert(err, IsNil)

	// Check settings can be read by every RU.
	for _, ru := range rus {
		m, err := ru.ReadSettings("peer/0")
		c.Assert(err, IsNil)
		c.Assert(m, DeepEquals, map[string]interface{}{"meme": "socially-awkward-penguin"})
	}
}

func (s *RelationUnitSuite) TestProReqSettings(c *C) {
	prr := NewProReqRelation(c, &s.ConnSuite, charm.ScopeGlobal)
	rus := RUs{prr.pru0, prr.pru1, prr.rru0, prr.rru1}

	// Check missing settings cannot be read by any RU.
	for _, ru := range rus {
		_, err := ru.ReadSettings("pro/0")
		c.Assert(err, ErrorMatches, `cannot read settings for unit "pro/0" in relation "pro:pname req:rname": not found`)
	}

	// Add settings for one RU.
	node, err := prr.pru0.Settings()
	c.Assert(err, IsNil)
	c.Assert(node.Map(), HasLen, 0)
	node.Set("meme", "foul-bachelor-frog")
	_, err = node.Write()
	c.Assert(err, IsNil)

	// Check settings can be read by every RU.
	for _, ru := range rus {
		m, err := ru.ReadSettings("pro/0")
		c.Assert(err, IsNil)
		c.Assert(m, DeepEquals, map[string]interface{}{"meme": "foul-bachelor-frog"})
	}
}

func (s *RelationUnitSuite) TestContainerSettings(c *C) {
	prr := NewProReqRelation(c, &s.ConnSuite, charm.ScopeContainer)
	rus := RUs{prr.pru0, prr.pru1, prr.rru0, prr.rru1}

	// Check missing settings cannot be read by any RU.
	for _, ru := range rus {
		_, err := ru.ReadSettings("pro/0")
		c.Assert(err, ErrorMatches, `cannot read settings for unit "pro/0" in relation "pro:pname req:rname": not found`)
	}

	// Add settings for one RU.
	node, err := prr.pru0.Settings()
	c.Assert(err, IsNil)
	c.Assert(node.Map(), HasLen, 0)
	node.Set("meme", "foul-bachelor-frog")
	_, err = node.Write()
	c.Assert(err, IsNil)

	// Check settings can be read by RUs in the same container.
	rus0 := RUs{prr.pru0, prr.rru0}
	for _, ru := range rus0 {
		m, err := ru.ReadSettings("pro/0")
		c.Assert(err, IsNil)
		c.Assert(m, DeepEquals, map[string]interface{}{"meme": "foul-bachelor-frog"})
	}

	// Check settings are still inaccessible to RUs outside that container
	rus1 := RUs{prr.pru1, prr.rru1}
	for _, ru := range rus1 {
		_, err := ru.ReadSettings("pro/0")
		c.Assert(err, ErrorMatches, `cannot read settings for unit "pro/0" in relation "pro:pname req:rname": not found`)
	}
}

func (s *RelationUnitSuite) TestPeerWatchScope(c *C) {
	pr := NewPeerRelation(c, &s.ConnSuite)

	// Test empty initial event.
	w0 := pr.ru0.WatchScope()
	defer stop(c, w0)
	s.assertScopeChange(c, w0, nil, nil)
	s.assertNoScopeChange(c, w0)

	// Join ru0, check no change; but private-address written.
	err := pr.ru0.EnsureJoin()
	c.Assert(err, IsNil)
	s.assertNoScopeChange(c, w0)
	node, err := pr.ru0.Settings()
	c.Assert(err, IsNil)
	c.Assert(node.Map(), DeepEquals, map[string]interface{}{"private-address": "peer-0.example.com"})

	// Join ru1, check change is observed.
	err = pr.ru1.EnsureJoin()
	c.Assert(err, IsNil)
	s.assertScopeChange(c, w0, []string{"peer/1"}, nil)
	s.assertNoScopeChange(c, w0)

	// Join ru1 again, check no problems and no changes.
	err = pr.ru1.EnsureJoin()
	c.Assert(err, IsNil)
	s.assertNoScopeChange(c, w0)

	// Stop watching, join ru2.
	stop(c, w0)
	err = pr.ru2.EnsureJoin()
	c.Assert(err, IsNil)

	// Start watch again, check initial event.
	w0 = pr.ru0.WatchScope()
	defer stop(c, w0)
	s.assertScopeChange(c, w0, []string{"peer/1", "peer/2"}, nil)
	s.assertNoScopeChange(c, w0)

	// Depart ru1, check event.
	err = pr.ru1.EnsureDepart()
	c.Assert(err, IsNil)
	s.assertScopeChange(c, w0, nil, []string{"peer/1"})
	s.assertNoScopeChange(c, w0)

	// Depart ru1 again, check no problems and no changes.
	err = pr.ru1.EnsureDepart()
	c.Assert(err, IsNil)
	s.assertNoScopeChange(c, w0)
}

func (s *RelationUnitSuite) TestProReqWatchScope(c *C) {
	prr := NewProReqRelation(c, &s.ConnSuite, charm.ScopeGlobal)

	// Test empty initial events for all RUs.
	ws := prr.watches()
	for _, w := range ws {
		defer stop(c, w)
	}
	for _, w := range ws {
		s.assertScopeChange(c, w, nil, nil)
	}
	s.assertNoScopeChange(c, ws...)

	// Join pru0, check detected only by req RUs.
	err := prr.pru0.EnsureJoin()
	c.Assert(err, IsNil)
	rws := func() []*state.RelationScopeWatcher {
		return []*state.RelationScopeWatcher{ws[2], ws[3]}
	}
	for _, w := range rws() {
		s.assertScopeChange(c, w, []string{"pro/0"}, nil)
	}
	s.assertNoScopeChange(c, ws...)

	// Join req0, check detected only by pro RUs.
	err = prr.rru0.EnsureJoin()
	c.Assert(err, IsNil)
	pws := func() []*state.RelationScopeWatcher {
		return []*state.RelationScopeWatcher{ws[0], ws[1]}
	}
	for _, w := range pws() {
		s.assertScopeChange(c, w, []string{"req/0"}, nil)
	}
	s.assertNoScopeChange(c, ws...)

	// Stop watches; join remaining RUs.
	for _, w := range ws {
		stop(c, w)
	}
	err = prr.pru1.EnsureJoin()
	c.Assert(err, IsNil)
	err = prr.rru1.EnsureJoin()
	c.Assert(err, IsNil)

	// Start new watches, check initial events.
	ws = prr.watches()
	for _, w := range ws {
		defer stop(c, w)
	}
	for _, w := range pws() {
		s.assertScopeChange(c, w, []string{"req/0", "req/1"}, nil)
	}
	for _, w := range rws() {
		s.assertScopeChange(c, w, []string{"pro/0", "pro/1"}, nil)
	}
	s.assertNoScopeChange(c, ws...)

	// Depart pru0, check detected only by req RUs.
	err = prr.pru0.EnsureDepart()
	c.Assert(err, IsNil)
	for _, w := range rws() {
		s.assertScopeChange(c, w, nil, []string{"pro/0"})
	}
	s.assertNoScopeChange(c, ws...)

	// Depart rru0, check detected only by pro RUs.
	err = prr.rru0.EnsureDepart()
	c.Assert(err, IsNil)
	for _, w := range pws() {
		s.assertScopeChange(c, w, nil, []string{"req/0"})
	}
	s.assertNoScopeChange(c, ws...)
}

func (s *RelationUnitSuite) TestContainerWatchScope(c *C) {
	prr := NewProReqRelation(c, &s.ConnSuite, charm.ScopeContainer)

	// Test empty initial events for all RUs.
	ws := prr.watches()
	for _, w := range ws {
		defer stop(c, w)
	}
	for _, w := range ws {
		s.assertScopeChange(c, w, nil, nil)
	}
	s.assertNoScopeChange(c, ws...)

	// Join pru0, check detected only by same-container req.
	err := prr.pru0.EnsureJoin()
	c.Assert(err, IsNil)
	s.assertScopeChange(c, ws[2], []string{"pro/0"}, nil)
	s.assertNoScopeChange(c, ws...)

	// Join req1, check detected only by same-container pro.
	err = prr.rru1.EnsureJoin()
	c.Assert(err, IsNil)
	s.assertScopeChange(c, ws[1], []string{"req/1"}, nil)
	s.assertNoScopeChange(c, ws...)

	// Stop watches; join remaining RUs.
	for _, w := range ws {
		stop(c, w)
	}
	err = prr.pru1.EnsureJoin()
	c.Assert(err, IsNil)
	err = prr.rru0.EnsureJoin()
	c.Assert(err, IsNil)

	// Start new watches, check initial events.
	ws = prr.watches()
	for _, w := range ws {
		defer stop(c, w)
	}
	s.assertScopeChange(c, ws[0], []string{"req/0"}, nil)
	s.assertScopeChange(c, ws[1], []string{"req/1"}, nil)
	s.assertScopeChange(c, ws[2], []string{"pro/0"}, nil)
	s.assertScopeChange(c, ws[3], []string{"pro/1"}, nil)
	s.assertNoScopeChange(c, ws...)

	// Depart pru0, check detected only by same-container req.
	err = prr.pru0.EnsureDepart()
	c.Assert(err, IsNil)
	s.assertScopeChange(c, ws[2], nil, []string{"pro/0"})
	s.assertNoScopeChange(c, ws...)

	// Depart rru0, check detected only by same-container pro.
	err = prr.rru0.EnsureDepart()
	c.Assert(err, IsNil)
	s.assertScopeChange(c, ws[0], nil, []string{"req/0"})
	s.assertNoScopeChange(c, ws...)
}

func (s *RelationUnitSuite) assertScopeChange(c *C, w *state.RelationScopeWatcher, added, removed []string) {
	s.State.StartSync()
	select {
	case ch, ok := <-w.Changes():
		c.Assert(ok, Equals, true)
		c.Assert(ch.Added, DeepEquals, added)
		c.Assert(ch.Removed, DeepEquals, removed)
	case <-time.After(500 * time.Millisecond):
		c.Fatalf("no change")
	}
}

func (s *RelationUnitSuite) assertNoScopeChange(c *C, ws ...*state.RelationScopeWatcher) {
	s.State.StartSync()
	for _, w := range ws {
		select {
		case ch, ok := <-w.Changes():
			c.Fatalf("got unwanted change: %#v, %t", ch, ok)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type PeerRelation struct {
	u0, u1, u2    *state.Unit
	ru0, ru1, ru2 *state.RelationUnit
}

func NewPeerRelation(c *C, s *ConnSuite) *PeerRelation {
	ch := s.AddTestingCharm(c, "dummy")
	svc, err := s.State.AddService("peer", ch)
	c.Assert(err, IsNil)
	ep := state.RelationEndpoint{"peer", "ifce", "name", state.RolePeer, charm.ScopeGlobal}
	rel, err := s.State.AddRelation(ep)
	c.Assert(err, IsNil)
	pr := &PeerRelation{}
	pr.u0, pr.ru0 = addRU(c, svc, rel, nil)
	pr.u1, pr.ru1 = addRU(c, svc, rel, nil)
	pr.u2, pr.ru2 = addRU(c, svc, rel, nil)
	return pr
}

type ProReqRelation struct {
	pu0, pu1, ru0, ru1     *state.Unit
	pru0, pru1, rru0, rru1 *state.RelationUnit
}

func NewProReqRelation(c *C, s *ConnSuite, scope charm.RelationScope) *ProReqRelation {
	ch := s.AddTestingCharm(c, "dummy")
	psvc, err := s.State.AddService("pro", ch)
	c.Assert(err, IsNil)
	if scope == charm.ScopeContainer {
		ch = s.AddTestingCharm(c, "logging")
	}
	rsvc, err := s.State.AddService("req", ch)
	c.Assert(err, IsNil)
	pep := state.RelationEndpoint{"pro", "ifce", "pname", state.RoleProvider, scope}
	rep := state.RelationEndpoint{"req", "ifce", "rname", state.RoleRequirer, scope}
	rel, err := s.State.AddRelation(pep, rep)
	c.Assert(err, IsNil)
	prr := &ProReqRelation{}
	prr.pu0, prr.pru0 = addRU(c, psvc, rel, nil)
	prr.pu1, prr.pru1 = addRU(c, psvc, rel, nil)
	if scope == charm.ScopeGlobal {
		prr.ru0, prr.rru0 = addRU(c, rsvc, rel, nil)
		prr.ru1, prr.rru1 = addRU(c, rsvc, rel, nil)
	} else {
		prr.ru0, prr.rru0 = addRU(c, rsvc, rel, prr.pu0)
		prr.ru1, prr.rru1 = addRU(c, rsvc, rel, prr.pu1)
	}
	return prr
}

func (prr *ProReqRelation) watches() []*state.RelationScopeWatcher {
	return []*state.RelationScopeWatcher{
		prr.pru0.WatchScope(), prr.pru1.WatchScope(),
		prr.rru0.WatchScope(), prr.rru1.WatchScope(),
	}
}

func addRU(c *C, svc *state.Service, rel *state.Relation, principal *state.Unit) (*state.Unit, *state.RelationUnit) {
	var u *state.Unit
	var err error
	if principal == nil {
		u, err = svc.AddUnit()
	} else {
		u, err = svc.AddUnitSubordinateTo(principal)
	}
	c.Assert(err, IsNil)
	addr := fmt.Sprintf("%s.example.com", strings.Replace(u.Name(), "/", "-", 1))
	err = u.SetPrivateAddress(addr)
	c.Assert(err, IsNil)
	ru, err := rel.Unit(u)
	c.Assert(err, IsNil)
	return u, ru
}

type RUs []*state.RelationUnit

type stopper interface {
	Stop() error
}

func stop(c *C, s stopper) {
	c.Assert(s.Stop(), IsNil)
}
