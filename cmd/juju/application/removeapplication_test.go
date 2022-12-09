// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package application

import (
	"bytes"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/juju/cmd/v3"
	"github.com/juju/cmd/v3/cmdtesting"
	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	apiapplication "github.com/juju/juju/api/client/application"
	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/cmd/cmdtest"
	"github.com/juju/juju/cmd/juju/application/mocks"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	"github.com/juju/juju/provider/dummy"
	"github.com/juju/juju/rpc/params"
	"github.com/juju/juju/testing"
)

type removeApplicationSuite struct {
	testing.FakeJujuXDGDataHomeSuite
	mockApi *mocks.MockRemoveApplicationAPI

	facadeVersion int

	apiFunc func() (RemoveApplicationAPI, error)
	store   *jujuclient.MemStore
}

var _ = gc.Suite(&removeApplicationSuite{})

func (s *removeApplicationSuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
	s.store = jujuclienttesting.MinimalStore()
	s.apiFunc = func() (RemoveApplicationAPI, error) {
		return s.mockApi, nil
	}
	s.facadeVersion = 16
}

func (s *removeApplicationSuite) setup(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.mockApi = mocks.NewMockRemoveApplicationAPI(ctrl)
	s.mockApi.EXPECT().BestAPIVersion().Return(s.facadeVersion).AnyTimes()
	s.mockApi.EXPECT().Close()

	return ctrl
}

func (s *removeApplicationSuite) runRemoveApplication(c *gc.C, args ...string) (*cmd.Context, error) {
	return cmdtesting.RunCommand(c, NewRemoveApplicationCommandForTest(s.apiFunc, s.store), args...)
}

func (s *removeApplicationSuite) runWithContext(ctx *cmd.Context, args ...string) (chan dummy.Operation, chan error) {
	remove := NewRemoveApplicationCommandForTest(s.apiFunc, s.store)
	return cmdtest.RunCommandWithDummyProvider(ctx, remove, args...)
}

func (s *removeApplicationSuite) TestRemoveApplication(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{},
	}}, nil)

	ctx, err := s.runRemoveApplication(c, "real-app")

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, "will remove application real-app\n")
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
}

func (s *removeApplicationSuite) TestRemoveApplicationForce(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
		Force:        true,
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{},
	}}, nil)

	ctx, err := s.runRemoveApplication(c, "real-app", "--force")

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, "will remove application real-app\n")
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
}

func (s *removeApplicationSuite) TestRemoveApplicationDryRun(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
		DryRun:       true,
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{},
	}}, nil)

	ctx, err := s.runRemoveApplication(c, "real-app", "--dry-run")

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
WARNING! This command:
`[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
will remove application real-app
`[1:])
}

func (s *removeApplicationSuite) TestRemoveApplicationDryRunOldFacade(c *gc.C) {
	s.facadeVersion = 15
	defer s.setup(c).Finish()

	_, err := s.runRemoveApplication(c, "real-app", "--dry-run")

	c.Assert(err, gc.ErrorMatches, "Your controller does not support `--dry-run`")
}

func (s *removeApplicationSuite) TestRemoveApplicationPrompt(c *gc.C) {
	defer s.setup(c).Finish()

	s.PatchEnvironment(osenv.JujuSkipConfirmationEnvKey, "0")

	var stdin bytes.Buffer
	ctx := cmdtesting.Context(c)
	ctx.Stdin = &stdin

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
		DryRun:       true,
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{},
	}}, nil)
	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{},
	}}, nil)

	stdin.WriteString("y")
	_, errc := s.runWithContext(ctx, "real-app")

	select {
	case err := <-errc:
		c.Check(err, jc.ErrorIsNil)
	case <-time.After(testing.LongWait):
		c.Fatal("command took too long")
	}

	c.Assert(cmdtesting.Stderr(ctx), gc.Matches, `
(?s)WARNING! This command:
.*`[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Matches, `
(?s)will remove application real-app
.*`[1:])
}

func (s *removeApplicationSuite) TestRemoveApplicationPromptOldFacade(c *gc.C) {
	s.facadeVersion = 15
	defer s.setup(c).Finish()

	s.PatchEnvironment(osenv.JujuSkipConfirmationEnvKey, "0")

	var stdin bytes.Buffer
	ctx := cmdtesting.Context(c)
	ctx.Stdin = &stdin

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{},
	}}, nil)

	stdin.WriteString("y")
	_, errc := s.runWithContext(ctx, "real-app")

	select {
	case err := <-errc:
		c.Check(err, jc.ErrorIsNil)
	case <-time.After(testing.LongWait):
		c.Fatal("command took too long")
	}

	c.Assert(cmdtesting.Stderr(ctx), gc.Matches, `(?s).*Your controller does not support a more in depth dry run.*`)
}

func setupRace(raceyApplications []string) func(args apiapplication.DestroyApplicationsParams) ([]params.DestroyApplicationResult, error) {
	return func(args apiapplication.DestroyApplicationsParams) ([]params.DestroyApplicationResult, error) {
		results := make([]params.DestroyApplicationResult, len(args.Applications))
		for i, app := range args.Applications {
			results[i].Info = &params.DestroyApplicationInfo{}
			for _, poison := range raceyApplications {
				if app == poison {
					err := errors.NewNotSupported(nil, "change detected")
					results[i].Error = apiservererrors.ServerError(err)
				}
			}
		}
		return results, nil
	}
}

func (s *removeApplicationSuite) TestHandlingNotSupportedDoesNotAffectBaseCase(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app"},
	}).DoAndReturn(setupRace([]string{"do-not-remove"}))

	ctx, err := s.runRemoveApplication(c, "real-app")

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, "will remove application real-app\n")
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
}

func (s *removeApplicationSuite) TestHandlingNotSupported(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"do-not-remove"},
	}).DoAndReturn(setupRace([]string{"do-not-remove"}))

	ctx, err := s.runRemoveApplication(c, "do-not-remove")

	c.Assert(err, gc.Equals, cmd.ErrSilent)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, "")
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
removing application do-not-remove failed: another user was updating application; please try again
`[1:])
}

func (s *removeApplicationSuite) TestHandlingNotSupportedMultipleApps(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"real-app", "do-not-remove", "another"},
	}).DoAndReturn(setupRace([]string{"do-not-remove"}))

	ctx, err := s.runRemoveApplication(c, "real-app", "do-not-remove", "another")

	c.Assert(err, gc.Equals, cmd.ErrSilent)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
will remove application real-app
will remove application another
`[1:])
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
removing application do-not-remove failed: another user was updating application; please try again
`[1:])
}

func (s *removeApplicationSuite) TestDetachStorage(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"storage-app"},
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{
			DetachedStorage: []params.Entity{{Tag: "storage-data-0"}, {Tag: "storage-data-1"}, {Tag: "storage-data-2"}, {Tag: "storage-data-3"}},
		},
	}}, nil)

	ctx, err := s.runRemoveApplication(c, "storage-app")

	c.Assert(err, jc.ErrorIsNil)
	stdout := cmdtesting.Stdout(ctx)
	c.Assert(stdout, gc.Equals, `
will remove application storage-app
- will detach storage data/0
- will detach storage data/1
- will detach storage data/2
- will detach storage data/3
`[1:])
}

func (s *removeApplicationSuite) TestDestroyStorage(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications:   []string{"storage-app"},
		DestroyStorage: true,
	}).Return([]params.DestroyApplicationResult{{
		Info: &params.DestroyApplicationInfo{
			DestroyedStorage: []params.Entity{{Tag: "storage-data-0"}, {Tag: "storage-data-1"}, {Tag: "storage-data-2"}, {Tag: "storage-data-3"}},
		},
	}}, nil)

	ctx, err := s.runRemoveApplication(c, "storage-app", "--destroy-storage")

	c.Assert(err, jc.ErrorIsNil)
	stdout := cmdtesting.Stdout(ctx)
	c.Assert(stdout, gc.Equals, `
will remove application storage-app
- will remove storage data/0
- will remove storage data/1
- will remove storage data/2
- will remove storage data/3
`[1:])
}

func (s *removeApplicationSuite) TestFailure(c *gc.C) {
	defer s.setup(c).Finish()

	s.mockApi.EXPECT().DestroyApplications(apiapplication.DestroyApplicationsParams{
		Applications: []string{"gargleblaster"},
	}).Return([]params.DestroyApplicationResult{{
		Error: &params.Error{
			Message: "doink",
		},
	}}, nil)

	ctx, err := s.runRemoveApplication(c, "gargleblaster")

	c.Assert(err, gc.Equals, cmd.ErrSilent)
	stderr := cmdtesting.Stderr(ctx)
	c.Assert(stderr, gc.Equals, `
removing application gargleblaster failed: doink
`[1:])
}

func (s *removeApplicationSuite) TestInvalidArgs(c *gc.C) {
	_, err := s.runRemoveApplication(c)
	c.Assert(err, gc.ErrorMatches, `no application specified`)

	_, err = s.runRemoveApplication(c, "invalid:name")
	c.Assert(err, gc.ErrorMatches, `invalid application name "invalid:name"`)
}

func (s *removeApplicationSuite) TestNoWaitWithoutForce(c *gc.C) {
	_, err := s.runRemoveApplication(c, "gargleblaster", "--no-wait")
	c.Assert(err, gc.ErrorMatches, `--no-wait without --force not valid`)
}
