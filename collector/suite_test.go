// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"github.com/globocom/config"
	"github.com/globocom/tsuru/app"
	"github.com/globocom/tsuru/db"
	ttesting "github.com/globocom/tsuru/testing"
	"labix.org/v2/mgo"
	. "launchpad.net/gocheck"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type S struct {
	session     *mgo.Session
	tmpdir      string
	instances   []string
	provisioner *ttesting.FakeProvisioner
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	var err error
	db.Session, err = db.Open("127.0.0.1:27017", "tsuru_collector_test")
	c.Assert(err, IsNil)
	s.provisioner = ttesting.NewFakeProvisioner()
	app.Provisioner = s.provisioner
	err = config.ReadConfigFile("../etc/tsuru.conf")
	c.Assert(err, IsNil)
	config.Set("queue-server", "127.0.0.1:0")
}

func (s *S) TearDownSuite(c *C) {
	db.Session.Apps().Database.DropDatabase()
	db.Session.Close()
}

func (s *S) TearDownTest(c *C) {
	_, err := db.Session.Apps().RemoveAll(nil)
	c.Assert(err, IsNil)
	s.provisioner.Reset()
}
