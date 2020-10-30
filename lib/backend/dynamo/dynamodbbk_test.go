// +build dynamodb

/*
Copyright 2015-2018 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

*/

package dynamo

import (
	"context"
	"testing"
	"time"

	"github.com/gravitational/teleport/lib/backend"
	"github.com/gravitational/teleport/lib/backend/test"
	"github.com/gravitational/teleport/lib/utils"

	"gopkg.in/check.v1"
)

func TestDynamoDB(t *testing.T) { check.TestingT(t) }

type DynamoDBSuite struct {
	bk        *Backend
	suite     test.BackendSuite
	tableName string
}

var _ = check.Suite(&DynamoDBSuite{})

func (s *DynamoDBSuite) SetUpSuite(c *check.C) {
	utils.InitLoggerForTests(testing.Verbose())
	var err error

	s.tableName = "teleport.dynamo.test"
	newBackend := func() (backend.Backend, error) {
		return New(context.Background(), map[string]interface{}{
			"table_name":         s.tableName,
			"poll_stream_period": 300 * time.Millisecond,
		})
	}
	bk, err := newBackend()
	c.Assert(err, check.IsNil)
	s.bk = bk.(*Backend)
	s.suite.B = s.bk
	s.suite.NewBackend = newBackend
}

// TearDownSuite stops the backend.
//
// TODO: This function should delete all tables created during tests.
func (s *DynamoDBSuite) TearDownSuite(c *check.C) {
	if s.bk != nil && s.bk.svc != nil {
		c.Assert(s.bk.Close(), check.IsNil)
	}
}

func (s *DynamoDBSuite) TestCRUD(c *check.C) {
	s.suite.CRUD(c)
}

func (s *DynamoDBSuite) TestRange(c *check.C) {
	s.suite.Range(c)
}

func (s *DynamoDBSuite) TestDeleteRange(c *check.C) {
	s.suite.DeleteRange(c)
}

func (s *DynamoDBSuite) TestCompareAndSwap(c *check.C) {
	s.suite.CompareAndSwap(c)
}

func (s *DynamoDBSuite) TestExpiration(c *check.C) {
	s.suite.Expiration(c)
}

func (s *DynamoDBSuite) TestKeepAlive(c *check.C) {
	s.suite.KeepAlive(c)
}

func (s *DynamoDBSuite) TestEvents(c *check.C) {
	s.suite.Events(c)
}

func (s *DynamoDBSuite) TestWatchersClose(c *check.C) {
	s.suite.WatchersClose(c)
}

func (s *DynamoDBSuite) TestLocking(c *check.C) {
	s.suite.Locking(c)
}

// TestContinuousBackups verifies that the continuous backup state is set upon
// startup of DynamoDB.
func (s *DynamoDBSuite) TestContinuousBackups(c *check.C) {
	var tests = []struct {
		enabled bool
		desc    check.CommentInterface
	}{
		{
			enabled: true,
			desc:    check.Commentf("enabled continuous backups"),
		},
		{
			enabled: false,
			desc:    check.Commentf("disabled continuous backups"),
		},
	}
	for _, tt := range tests {
		tableName := "teleport.dynamo.continuous.backups"
		newBackend := func() (backend.Backend, error) {
			return New(context.Background(), map[string]interface{}{
				"table_name":         tableName,
				"continuous_backups": tt.enabled,
			})
		}
		bk, err := newBackend()
		c.Assert(err, check.IsNil, tt.desc)
		d := bk.(*Backend)

		ok, err := d.getContinuousBackups(context.Background())
		c.Assert(err, check.IsNil, tt.desc)
		c.Assert(ok, check.Equals, tt.enabled, tt.desc)
	}
}

// TestContinuousBackups verifies that auto scaling is enabled and disabled
// upon startup of DynamoDB.
func (s *DynamoDBSuite) TestAutoScaling(c *check.C) {
	var tests = []struct {
		inEnabled          bool
		inReadMinCapacity  int
		inReadMaxCapacity  int
		inReadTargetValue  float64
		inWriteMinCapacity int
		inWriteMaxCapacity int
		inWriteTargetValue float64
		desc               check.CommentInterface
	}{
		{
			inEnabled:          true,
			inReadMinCapacity:  10,
			inReadMaxCapacity:  20,
			inReadTargetValue:  50.0,
			inWriteMinCapacity: 10,
			inWriteMaxCapacity: 20,
			inWriteTargetValue: 50.0,
			desc:               check.Commentf("enabled auto scaling"),
		},
		{
			inEnabled:          false,
			inReadMinCapacity:  0,
			inReadMaxCapacity:  0,
			inReadTargetValue:  0.0,
			inWriteMinCapacity: 0,
			inWriteMaxCapacity: 0,
			inWriteTargetValue: 0.0,
			desc:               check.Commentf("disabled auto scaling"),
		},
	}
	for _, tt := range tests {
		tableName := "teleport.dynamo.continuous.backups"
		newBackend := func() (backend.Backend, error) {
			return New(context.Background(), map[string]interface{}{
				"table_name":         tableName,
				"auto_scaling":       tt.inEnabled,
				"read_min_capacity":  tt.inReadMinCapacity,
				"read_max_capacity":  tt.inReadMaxCapacity,
				"read_target_value":  tt.inReadTargetValue,
				"write_min_capacity": tt.inWriteMinCapacity,
				"write_max_capacity": tt.inWriteMaxCapacity,
				"write_target_value": tt.inWriteTargetValue,
			})
		}
		bk, err := newBackend()
		c.Assert(err, check.IsNil, tt.desc)
		d := bk.(*Backend)

		resp, err := d.getAutoScaling(context.Background())
		c.Assert(err, check.IsNil, tt.desc)
		c.Assert(resp.readMinCapacity, check.Equals, tt.inReadMinCapacity)
		c.Assert(resp.readMaxCapacity, check.Equals, tt.inReadMaxCapacity)
		c.Assert(resp.readTargetValue, check.Equals, tt.inReadTargetValue)
		c.Assert(resp.writeMinCapacity, check.Equals, tt.inWriteMinCapacity)
		c.Assert(resp.writeMaxCapacity, check.Equals, tt.inWriteMaxCapacity)
		c.Assert(resp.writeTargetValue, check.Equals, tt.inWriteTargetValue)
	}
}
