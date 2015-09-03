// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package client_test

import (
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/params"
)

func (s *serverSuite) TestGetBundleChangesBundleContentError(c *gc.C) {
	args := params.GetBundleChangesParams{
		BundleDataYAML: ":",
	}
	r, err := s.client.GetBundleChanges(args)
	c.Assert(err, gc.ErrorMatches, `cannot read bundle YAML: cannot unmarshal bundle data: YAML error: did not find expected key`)
	c.Assert(r, gc.DeepEquals, params.GetBundleChangesResults{})
}

func (s *serverSuite) TestGetBundleChangesBundleVerificationErrors(c *gc.C) {
	args := params.GetBundleChangesParams{
		BundleDataYAML: `
            services:
                django:
                    charm: django
                    to: [1]
                haproxy:
                    charm: 42
                    num_units: -1
        `,
	}
	r, err := s.client.GetBundleChanges(args)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(r.Changes, gc.IsNil)
	c.Assert(r.Errors, jc.SameContents, []string{
		`placement "1" refers to a machine not defined in this bundle`,
		`too many units specified in unit placement for service "django"`,
		`invalid charm URL in service "haproxy": charm URL has invalid charm name: "42"`,
		`negative number of units specified on service "haproxy"`,
	})
}

func (s *serverSuite) TestGetBundleChangesSuccess(c *gc.C) {
	args := params.GetBundleChangesParams{
		BundleDataYAML: `
            services:
                django:
                    charm: django
                    options:
                        debug: true
                haproxy:
                    charm: cs:trusty/haproxy-42
            relations:
                - - django:web
                  - haproxy:web
        `,
	}
	r, err := s.client.GetBundleChanges(args)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(r.Changes, jc.DeepEquals, []*params.BundleChangesChange{{
		Id:     "addCharm-0",
		Method: "addCharm",
		Args:   []interface{}{"django"},
	}, {
		Id:       "deploy-1",
		Method:   "deploy",
		Args:     []interface{}{"django", "django", map[string]interface{}{"debug": true}},
		Requires: []string{"addCharm-0"},
	}, {
		Id:     "addCharm-2",
		Method: "addCharm",
		Args:   []interface{}{"cs:trusty/haproxy-42"},
	}, {
		Id:       "deploy-3",
		Method:   "deploy",
		Args:     []interface{}{"cs:trusty/haproxy-42", "haproxy", map[string]interface{}{}},
		Requires: []string{"addCharm-2"},
	}, {
		Id:       "addRelation-4",
		Method:   "addRelation",
		Args:     []interface{}{"$deploy-1:web", "$deploy-3:web"},
		Requires: []string{"deploy-1", "deploy-3"},
	}})
	c.Assert(r.Errors, gc.IsNil)
}
