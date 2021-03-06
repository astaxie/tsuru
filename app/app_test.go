// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/globocom/config"
	"github.com/globocom/tsuru/api/auth"
	"github.com/globocom/tsuru/api/bind"
	"github.com/globocom/tsuru/db"
	"github.com/globocom/tsuru/log"
	"github.com/globocom/tsuru/provision"
	"github.com/globocom/tsuru/queue"
	"github.com/globocom/tsuru/repository"
	"github.com/globocom/tsuru/testing"
	"labix.org/v2/mgo/bson"
	. "launchpad.net/gocheck"
	stdlog "log"
	"os"
	"path"
	"strings"
	"time"
)

func (s *S) TestGet(c *C) {
	newApp := App{Name: "myApp", Framework: "Django"}
	err := db.Session.Apps().Insert(newApp)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": newApp.Name})
	newApp.Env = map[string]bind.EnvVar{}
	newApp.Logs = []Applog{}
	err = db.Session.Apps().Update(bson.M{"name": newApp.Name}, &newApp)
	c.Assert(err, IsNil)
	myApp := App{Name: "myApp"}
	err = myApp.Get()
	c.Assert(err, IsNil)
	c.Assert(myApp.Name, Equals, newApp.Name)
	c.Assert(myApp.State, Equals, newApp.State)
}

func (s *S) TestDestroy(c *C) {
	h := testHandler{}
	ts := s.t.StartGandalfTestServer(&h)
	defer ts.Close()
	a := App{
		Name:      "ritual",
		Framework: "ruby",
		Teams:     []string{s.team.Name},
		Units: []Unit{
			{
				Name:    "duvido",
				Machine: 3,
			},
		},
	}
	err := CreateApp(&a, 1)
	c.Assert(err, IsNil)
	err = a.Destroy()
	c.Assert(err, IsNil)
	err = a.Get()
	c.Assert(err, NotNil)
	qt, err := db.Session.Apps().Find(bson.M{"name": a.Name}).Count()
	c.Assert(err, IsNil)
	c.Assert(qt, Equals, 0)
	c.Assert(s.provisioner.FindApp(&a), Equals, -1)
}

func (s *S) TestDestroyWithoutUnits(c *C) {
	h := testHandler{}
	ts := s.t.StartGandalfTestServer(&h)
	defer ts.Close()
	app := App{Name: "x4"}
	err := CreateApp(&app, 1)
	c.Assert(err, IsNil)
	err = app.Destroy()
	c.Assert(err, IsNil)
}

func (s *S) TestFailingDestroy(c *C) {
	h := testHandler{}
	ts := s.t.StartGandalfTestServer(&h)
	defer ts.Close()
	s.provisioner.PrepareFailure("Destroy", errors.New("will not destroy this app!"))
	a := App{
		Name:      "ritual",
		Framework: "ruby",
		Teams:     []string{s.team.Name},
		Units:     []Unit{{Name: "duvido", Machine: 3}},
	}
	err := CreateApp(&a, 1)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": "ritual"})
	err = a.Destroy()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Failed to destroy the app: will not destroy this app!")
}

// TODO(fss): simplify this test. Right now, it's a little monster.
func (s *S) TestCreateApp(c *C) {
	patchRandomReader()
	defer unpatchRandomReader()
	h := testHandler{}
	ts := s.t.StartGandalfTestServer(&h)
	defer ts.Close()
	server := testing.FakeQueueServer{}
	server.Start("127.0.0.1:0")
	defer server.Stop()
	a := App{
		Name:      "appname",
		Framework: "django",
		Units:     []Unit{{Machine: 3}},
	}
	expectedHost := "localhost"
	config.Set("host", expectedHost)
	old, err := config.Get("queue-server")
	if err != nil {
		defer config.Set("queue-server", old)
	}
	config.Set("queue-server", server.Addr())

	err = CreateApp(&a, 3)
	c.Assert(err, IsNil)
	defer a.Destroy()
	c.Assert(a.State, Equals, "pending")
	var retrievedApp App
	err = db.Session.Apps().Find(bson.M{"name": a.Name}).One(&retrievedApp)
	c.Assert(err, IsNil)
	c.Assert(retrievedApp.Name, Equals, a.Name)
	c.Assert(retrievedApp.Framework, Equals, a.Framework)
	c.Assert(retrievedApp.State, Equals, a.State)
	env := a.InstanceEnv(s3InstanceName)
	c.Assert(env["TSURU_S3_ENDPOINT"].Value, Equals, s.t.S3Server.URL())
	c.Assert(env["TSURU_S3_ENDPOINT"].Public, Equals, false)
	c.Assert(env["TSURU_S3_LOCATIONCONSTRAINT"].Value, Equals, "true")
	c.Assert(env["TSURU_S3_LOCATIONCONSTRAINT"].Public, Equals, false)
	e, ok := env["TSURU_S3_ACCESS_KEY_ID"]
	c.Assert(ok, Equals, true)
	c.Assert(e.Public, Equals, false)
	e, ok = env["TSURU_S3_SECRET_KEY"]
	c.Assert(ok, Equals, true)
	c.Assert(e.Public, Equals, false)
	c.Assert(env["TSURU_S3_BUCKET"].Value, HasLen, maxBucketSize)
	c.Assert(env["TSURU_S3_BUCKET"].Value, Equals, "appnamee3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3e3")
	c.Assert(env["TSURU_S3_BUCKET"].Public, Equals, false)
	env = a.InstanceEnv("")
	c.Assert(env["APPNAME"].Value, Equals, a.Name)
	c.Assert(env["APPNAME"].Public, Equals, false)
	c.Assert(env["TSURU_HOST"].Value, Equals, expectedHost)
	c.Assert(env["TSURU_HOST"].Public, Equals, false)
	expectedMessage := queue.Message{
		Action: RegenerateApprc,
		Args:   []string{a.Name},
	}
	c.Assert(server.Messages(), DeepEquals, []queue.Message{expectedMessage})
	c.Assert(s.provisioner.GetUnits(&a), HasLen, 3)
}

func (s *S) TestCantCreateAppWithZeroUnits(c *C) {
	a := App{Name: "paradisum"}
	err := CreateApp(&a, 0)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Cannot create app with 0 units.")
}

func (s *S) TestCantCreateTwoAppsWithTheSameName(c *C) {
	err := db.Session.Apps().Insert(bson.M{"name": "appName"})
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": "appName"})
	a := App{Name: "appName"}
	err = CreateApp(&a, 1)
	defer a.Destroy() // clean mess if test fail
	c.Assert(err, NotNil)
}

func (s *S) TestCantCreateAppWithInvalidName(c *C) {
	a := App{
		Name:      "1123app",
		Framework: "ruby",
	}
	err := CreateApp(&a, 1)
	c.Assert(err, NotNil)
	e, ok := err.(*ValidationError)
	c.Assert(ok, Equals, true)
	msg := "Invalid app name, your app should have at most 63 " +
		"characters, containing only lower case letters or numbers, " +
		"starting with a letter."
	c.Assert(e.Message, Equals, msg)
}

func (s *S) TestDoesNotSaveTheAppInTheDatabaseIfProvisionerFail(c *C) {
	h := testHandler{}
	ts := s.t.StartGandalfTestServer(&h)
	defer ts.Close()
	s.provisioner.PrepareFailure("Provision", errors.New("exit status 1"))
	a := App{
		Name:      "theirapp",
		Framework: "ruby",
		Units:     []Unit{{Machine: 1}},
	}
	err := CreateApp(&a, 1)
	defer a.Destroy() // clean mess if test fail
	c.Assert(err, NotNil)
	expected := `exit status 1`
	c.Assert(err.Error(), Equals, expected)
	err = a.Get()
	c.Assert(err, NotNil)
}

func (s *S) TestDeletesIAMCredentialsAndS3BucketIfProvisionerFail(c *C) {
	h := testHandler{}
	ts := s.t.StartGandalfTestServer(&h)
	defer ts.Close()
	s.provisioner.PrepareFailure("Provision", errors.New("exit status 1"))
	source := patchRandomReader()
	defer unpatchRandomReader()
	a := App{
		Name:      "theirapp",
		Framework: "ruby",
		Units:     []Unit{{Machine: 1}},
	}
	err := CreateApp(&a, 1)
	defer a.Destroy() // clean mess if test fail
	c.Assert(err, NotNil)
	iam := getIAMEndpoint()
	_, err = iam.GetUser("theirapp")
	c.Assert(err, NotNil)
	s3 := getS3Endpoint()
	bucketName := fmt.Sprintf("%s%x", a.Name, source[:(maxBucketSize-len(a.Name)/2)])
	bucket := s3.Bucket(bucketName)
	_, err = bucket.Get("")
	c.Assert(err, NotNil)
}

func (s *S) TestAppendOrUpdate(c *C) {
	a := App{
		Name:      "appName",
		Framework: "django",
	}
	u := Unit{Name: "i-00000zz8", Ip: "", Machine: 1}
	a.AddUnit(&u)
	c.Assert(len(a.Units), Equals, 1)
	u = Unit{
		Name:  "i-00000zz8",
		Ip:    "192.168.0.12",
		State: string(provision.StatusStarted),
	}
	a.AddUnit(&u)
	c.Assert(len(a.Units), Equals, 1)
	c.Assert(a.Units[0], DeepEquals, u)
}

func (s *S) TestAddUnits(c *C) {
	server := testing.FakeQueueServer{}
	server.Start("127.0.0.1:0")
	defer server.Stop()
	old, err := config.Get("queue-server")
	if err != nil {
		defer config.Set("queue-server", old)
	}
	config.Set("queue-server", server.Addr())
	app := App{Name: "warpaint", Framework: "python"}
	err = db.Session.Apps().Insert(app)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app.Name})
	s.provisioner.Provision(&app)
	defer s.provisioner.Destroy(&app)
	err = app.AddUnits(5)
	c.Assert(err, IsNil)
	units := s.provisioner.GetUnits(&app)
	c.Assert(units, HasLen, 6)
	err = app.AddUnits(2)
	c.Assert(err, IsNil)
	units = s.provisioner.GetUnits(&app)
	c.Assert(units, HasLen, 8)
	for _, unit := range units {
		c.Assert(unit.AppName, Equals, app.Name)
	}
	err = app.Get()
	c.Assert(err, IsNil)
	c.Assert(app.Units, HasLen, 7)
	var expectedMessages []queue.Message
	names := make([]string, len(app.Units))
	for i, unit := range app.Units {
		names[i] = unit.Name
		expected := fmt.Sprintf("%s/%d", app.Name, i+1)
		c.Assert(unit.Name, Equals, expected)
		messages := []queue.Message{
			{Action: RegenerateApprc, Args: []string{app.Name, unit.Name}},
			{Action: StartApp, Args: []string{app.Name, unit.Name}},
		}
		expectedMessages = append(expectedMessages, messages...)
	}
	time.Sleep(1e6)
	c.Assert(server.Messages(), DeepEquals, expectedMessages)
}

func (s *S) TestAddZeroUnits(c *C) {
	app := App{Name: "warpaint", Framework: "ruby"}
	err := app.AddUnits(0)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Cannot add zero units.")
}

func (s *S) TestAddUnitsFailureInProvisioner(c *C) {
	app := App{Name: "scars", Framework: "golang"}
	err := app.AddUnits(2)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "App is not provisioned.")
}

func (s *S) TestRemoveUnits(c *C) {
	app := App{
		Name:      "chemistry",
		Framework: "python",
		Units: []Unit{
			{Name: "chemistry/0"},
			{Name: "chemistry/1"},
			{Name: "chemistry/2"},
			{Name: "chemistry/3"},
		},
	}
	err := db.Session.Apps().Insert(app)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app.Name})
	s.provisioner.Provision(&app)
	defer s.provisioner.Destroy(&app)
	s.provisioner.AddUnits(&app, 4)
	err = app.RemoveUnits(2)
	c.Assert(err, IsNil)
	units := s.provisioner.GetUnits(&app)
	c.Assert(units, HasLen, 3)
	c.Assert(units[0].Name, Equals, "chemistry/2")
	c.Assert(units[1].Name, Equals, "chemistry/3")
	c.Assert(units[2].Name, Equals, "chemistry/4")
	err = app.Get()
	c.Assert(err, IsNil)
	c.Assert(app.Units, HasLen, 2)
	c.Assert(app.Units[0].Name, Equals, "chemistry/2")
	c.Assert(app.Units[1].Name, Equals, "chemistry/3")
}

func (s *S) TestRemoveUnitsInvalidValues(c *C) {
	var tests = []struct {
		n        uint
		expected string
	}{
		{0, "Cannot remove zero units."},
		{4, "Cannot remove all units from an app."},
		{5, "Cannot remove 5 units from this app, it has only 4 units."},
	}
	app := App{
		Name:      "chemistry",
		Framework: "python",
		Units: []Unit{
			{Name: "chemistry/0"},
			{Name: "chemistry/1"},
			{Name: "chemistry/2"},
			{Name: "chemistry/3"},
		},
	}
	err := db.Session.Apps().Insert(app)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app.Name})
	s.provisioner.Provision(&app)
	defer s.provisioner.Destroy(&app)
	s.provisioner.AddUnits(&app, 4)
	for _, test := range tests {
		err := app.RemoveUnits(test.n)
		c.Check(err, NotNil)
		c.Check(err.Error(), Equals, test.expected)
	}
}

func (s *S) TestRemoveUnitsFailureInProvisioner(c *C) {
	s.provisioner.PrepareFailure("RemoveUnits", errors.New("Cannot remove these units."))
	app := App{
		Name:      "paradisum",
		Framework: "python",
		Units:     []Unit{{Name: "paradisum/0"}, {Name: "paradisum/1"}},
	}
	err := db.Session.Apps().Insert(app)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app.Name})
	s.provisioner.Provision(&app)
	defer s.provisioner.Destroy(&app)
	err = app.RemoveUnits(1)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Cannot remove these units.")
}

func (s *S) TestRemoveUnitsFromIndicesSlice(c *C) {
	var tests = []struct {
		input    []Unit
		indices  []int
		expected []Unit
	}{
		{
			input:    []Unit{{Name: "unit1"}, {Name: "unit2"}, {Name: "unit3"}, {Name: "unit4"}},
			indices:  []int{0, 1, 2},
			expected: []Unit{{Name: "unit4"}},
		},
		{
			input:    []Unit{{Name: "unit1"}, {Name: "unit2"}, {Name: "unit3"}, {Name: "unit4"}},
			indices:  []int{0, 3, 4},
			expected: []Unit{{Name: "unit2"}},
		},
		{
			input:    []Unit{{Name: "unit1"}, {Name: "unit2"}, {Name: "unit3"}, {Name: "unit4"}},
			indices:  []int{4},
			expected: []Unit{{Name: "unit1"}, {Name: "unit2"}, {Name: "unit3"}},
		},
	}
	for _, t := range tests {
		a := App{Units: t.input}
		a.removeUnits(t.indices)
		c.Check(a.Units, DeepEquals, t.expected)
	}
}

func (s *S) TestGrantAccess(c *C) {
	a := App{Name: "appName", Framework: "django", Teams: []string{}}
	err := a.Grant(&s.team)
	c.Assert(err, IsNil)
	_, found := a.Find(&s.team)
	c.Assert(found, Equals, true)
}

func (s *S) TestGrantAccessKeepTeamsSorted(c *C) {
	a := App{Name: "appName", Framework: "django", Teams: []string{"acid-rain", "zito"}}
	err := a.Grant(&s.team)
	c.Assert(err, IsNil)
	c.Assert(a.Teams, DeepEquals, []string{"acid-rain", s.team.Name, "zito"})
}

func (s *S) TestGrantAccessFailsIfTheTeamAlreadyHasAccessToTheApp(c *C) {
	a := App{Name: "appName", Framework: "django", Teams: []string{s.team.Name}}
	err := a.Grant(&s.team)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^This team already has access to this app$")
}

func (s *S) TestRevokeAccess(c *C) {
	a := App{Name: "appName", Framework: "django", Teams: []string{s.team.Name}}
	err := a.Revoke(&s.team)
	c.Assert(err, IsNil)
	_, found := a.Find(&s.team)
	c.Assert(found, Equals, false)
}

func (s *S) TestRevoke(c *C) {
	a := App{Name: "test", Teams: []string{"team1", "team2", "team3", "team4"}}
	err := a.Revoke(&auth.Team{Name: "team2"})
	c.Assert(err, IsNil)
	c.Assert(a.Teams, DeepEquals, []string{"team1", "team3", "team4"})
	err = a.Revoke(&auth.Team{Name: "team4"})
	c.Assert(err, IsNil)
	c.Assert(a.Teams, DeepEquals, []string{"team1", "team3"})
	err = a.Revoke(&auth.Team{Name: "team1"})
	c.Assert(err, IsNil)
	c.Assert(a.Teams, DeepEquals, []string{"team3"})
}

func (s *S) TestRevokeAccessFailsIfTheTeamsDoesNotHaveAccessToTheApp(c *C) {
	a := App{Name: "appName", Framework: "django", Teams: []string{}}
	err := a.Revoke(&s.team)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^This team does not have access to this app$")
}

func (s *S) TestSetEnvNewAppsTheMapIfItIsNil(c *C) {
	a := App{Name: "how-many-more-times"}
	c.Assert(a.Env, IsNil)
	env := bind.EnvVar{Name: "PATH", Value: "/"}
	a.setEnv(env)
	c.Assert(a.Env, NotNil)
}

func (s *S) TestSetEnvironmentVariableToApp(c *C) {
	a := App{Name: "appName", Framework: "django"}
	a.setEnv(bind.EnvVar{Name: "PATH", Value: "/", Public: true})
	env := a.Env["PATH"]
	c.Assert(env.Name, Equals, "PATH")
	c.Assert(env.Value, Equals, "/")
	c.Assert(env.Public, Equals, true)
}

func (s *S) TestSetEnvRespectsThePublicOnlyFlagKeepPrivateVariablesWhenItsTrue(c *C) {
	a := App{
		Name:  "myapp",
		Units: []Unit{{Machine: 1}},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	envs := []bind.EnvVar{
		{
			Name:   "DATABASE_HOST",
			Value:  "remotehost",
			Public: false,
		},
		{
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	err = a.SetEnvsToApp(envs, true, false)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "localhost",
			Public: false,
		},
		"DATABASE_PASSWORD": {
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	c.Assert(newApp.Env, DeepEquals, expected)
}

func (s *S) TestSetEnvRespectsThePublicOnlyFlagOverwrittenAllVariablesWhenItsFalse(c *C) {
	a := App{
		Name: "myapp",
		Units: []Unit{
			{Machine: 1},
		},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	envs := []bind.EnvVar{
		{
			Name:   "DATABASE_HOST",
			Value:  "remotehost",
			Public: true,
		},
		{
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	err = a.SetEnvsToApp(envs, false, false)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "remotehost",
			Public: true,
		},
		"DATABASE_PASSWORD": {
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	c.Assert(newApp.Env, DeepEquals, expected)
}

func (s *S) TestUnsetEnvRespectsThePublicOnlyFlagKeepPrivateVariablesWhenItsTrue(c *C) {
	a := App{
		Name: "myapp",
		Units: []Unit{
			{Machine: 1},
		},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
			"DATABASE_PASSWORD": {
				Name:   "DATABASE_PASSWORD",
				Value:  "123",
				Public: true,
			},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = a.UnsetEnvsFromApp([]string{"DATABASE_HOST", "DATABASE_PASSWORD"}, true, false)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "localhost",
			Public: false,
		},
	}
	c.Assert(newApp.Env, DeepEquals, expected)
}

func (s *S) TestUnsetEnvRespectsThePublicOnlyFlagUnsettingAllVariablesWhenItsFalse(c *C) {
	a := App{
		Name: "myapp",
		Units: []Unit{
			{Machine: 1},
		},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
			"DATABASE_PASSWORD": {
				Name:   "DATABASE_PASSWORD",
				Value:  "123",
				Public: true,
			},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = a.UnsetEnvsFromApp([]string{"DATABASE_HOST", "DATABASE_PASSWORD"}, false, false)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	c.Assert(newApp.Env, DeepEquals, map[string]bind.EnvVar{})
}

func (s *S) TestGetEnvironmentVariableFromApp(c *C) {
	a := App{Name: "whole-lotta-love"}
	a.setEnv(bind.EnvVar{Name: "PATH", Value: "/"})
	v, err := a.getEnv("PATH")
	c.Assert(err, IsNil)
	c.Assert(v.Value, Equals, "/")
}

func (s *S) TestGetEnvReturnsErrorIfTheVariableIsNotDeclared(c *C) {
	a := App{Name: "what-is-and-what-should-never"}
	a.Env = make(map[string]bind.EnvVar)
	_, err := a.getEnv("PATH")
	c.Assert(err, NotNil)
}

func (s *S) TestGetEnvReturnsErrorIfTheEnvironmentMapIsNil(c *C) {
	a := App{Name: "what-is-and-what-should-never"}
	_, err := a.getEnv("PATH")
	c.Assert(err, NotNil)
}

func (s *S) TestInstanceEnvironmentReturnEnvironmentVariablesForTheServer(c *C) {
	envs := map[string]bind.EnvVar{
		"DATABASE_HOST": {Name: "DATABASE_HOST", Value: "localhost", Public: false, InstanceName: "mysql"},
		"DATABASE_USER": {Name: "DATABASE_USER", Value: "root", Public: true, InstanceName: "mysql"},
		"HOST":          {Name: "HOST", Value: "10.0.2.1", Public: false, InstanceName: "redis"},
	}
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {Name: "DATABASE_HOST", Value: "localhost", Public: false, InstanceName: "mysql"},
		"DATABASE_USER": {Name: "DATABASE_USER", Value: "root", Public: true, InstanceName: "mysql"},
	}
	a := App{Name: "hi-there", Env: envs}
	c.Assert(a.InstanceEnv("mysql"), DeepEquals, expected)
}

func (s *S) TestInstanceEnvironmentDoesNotPanicIfTheEnvMapIsNil(c *C) {
	a := App{Name: "hi-there"}
	c.Assert(a.InstanceEnv("mysql"), DeepEquals, map[string]bind.EnvVar{})
}

func (s *S) TestIsValid(c *C) {
	var data = []struct {
		name     string
		expected bool
	}{
		{"myappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyapp", false},
		{"myappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyap", false},
		{"myappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyappmyappmya", true},
		{"myApp", false},
		{"my app", false},
		{"123myapp", false},
		{"myapp", true},
		{"_theirapp", false},
		{"my-app", false},
		{"my_app", false},
		{"b", true},
	}
	for _, d := range data {
		a := App{Name: d.name}
		if valid := a.isValid(); valid != d.expected {
			c.Errorf("Is %q a valid app name? Expected: %v. Got: %v.", d.name, d.expected, valid)
		}
	}
}

func (s *S) TestUnit(c *C) {
	u := Unit{Name: "someapp/0", Type: "django", Machine: 10}
	a := App{Name: "appName", Framework: "django", Units: []Unit{u}}
	u2 := a.Unit()
	u.app = &a
	c.Assert(*u2, DeepEquals, u)
}

func (s *S) TestEmptyUnit(c *C) {
	a := App{Name: "myApp"}
	expected := Unit{app: &a}
	unit := a.Unit()
	c.Assert(*unit, DeepEquals, expected)
}

func (s *S) TestDeployHookAbsPath(c *C) {
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	old, err := config.Get("git:unit-repo")
	c.Assert(err, IsNil)
	config.Set("git:unit-repo", pwd)
	defer config.Set("git:unit-repo", old)
	expected := path.Join(pwd, "testdata", "pre.sh")
	command := "testdata/pre.sh"
	got, err := deployHookAbsPath(command)
	c.Assert(err, IsNil)
	c.Assert(got, Equals, expected)
}

func (s *S) TestDeployHookAbsPathAbsoluteCommands(c *C) {
	command := "python manage.py syncdb --noinput"
	expected := "python manage.py syncdb --noinput"
	got, err := deployHookAbsPath(command)
	c.Assert(err, IsNil)
	c.Assert(got, Equals, expected)
}

func (s *S) TestLoadHooks(c *C) {
	output := `pre-restart:
  - testdata/pre.sh
pos-restart:
  - testdata/pos.sh
`
	s.provisioner.PrepareOutput([]byte(output))
	a := App{
		Name:      "something",
		Framework: "django",
		State:     string(provision.StatusStarted),
	}
	err := a.loadHooks()
	c.Assert(err, IsNil)
	c.Assert(a.hooks.PreRestart, DeepEquals, []string{"testdata/pre.sh"})
	c.Assert(a.hooks.PosRestart, DeepEquals, []string{"testdata/pos.sh"})
}

func (s *S) TestLoadHooksWithListOfCommands(c *C) {
	output := `pre-restart:
  - testdata/pre.sh
  - ls -lh
  - sudo rm -rf /
pos-restart:
  - testdata/pos.sh
`
	s.provisioner.PrepareOutput([]byte(output))
	a := App{
		Name:      "something",
		Framework: "django",
		State:     string(provision.StatusStarted),
	}
	err := a.loadHooks()
	c.Assert(err, IsNil)
	c.Assert(a.hooks.PreRestart, DeepEquals, []string{"testdata/pre.sh", "ls -lh", "sudo rm -rf /"})
	c.Assert(a.hooks.PosRestart, DeepEquals, []string{"testdata/pos.sh"})
}

func (s *S) TestLoadHooksWithError(c *C) {
	a := App{Name: "something", Framework: "django"}
	err := a.loadHooks()
	c.Assert(err, IsNil)
	c.Assert(a.hooks.PreRestart, IsNil)
	c.Assert(a.hooks.PosRestart, IsNil)
}

func (s *S) TestPreRestart(c *C) {
	s.provisioner.PrepareOutput([]byte("pre-restarted"))
	a := App{
		Name:      "something",
		Framework: "django",
		State:     string(provision.StatusStarted),
		hooks: &conf{
			PreRestart: []string{"pre.sh"},
			PosRestart: []string{"pos.sh"},
		},
	}
	w := new(bytes.Buffer)
	err := a.preRestart(w)
	c.Assert(err, IsNil)
	c.Assert(err, IsNil)
	st := strings.Replace(w.String(), "\n", "###", -1)
	c.Assert(st, Matches, `.*### ---> Running pre-restart###.*pre-restarted$`)
	cmds := s.provisioner.GetCmds("", &a)
	c.Assert(cmds, HasLen, 1)
	c.Assert(cmds[0].Cmd, Matches, `^\[ -f /home/application/apprc \] && source /home/application/apprc; \[ -d /home/application/current \] && cd /home/application/current;.*pre.sh$`)
}

func (s *S) TestPreRestartWhenAppConfDoesNotExist(c *C) {
	a := App{Name: "something", Framework: "django"}
	w := new(bytes.Buffer)
	l := stdlog.New(w, "", stdlog.LstdFlags)
	log.SetLogger(l)
	err := a.preRestart(w)
	c.Assert(err, IsNil)
	st := strings.Split(w.String(), "\n")
	regexp := ".*Skipping pre-restart hooks..."
	c.Assert(st[len(st)-2], Matches, regexp)
}

func (s *S) TestSkipsPreRestartWhenPreRestartSectionDoesNotExists(c *C) {
	a := App{
		Name:      "something",
		Framework: "django",
		Units:     []Unit{{State: string(provision.StatusStarted), Machine: 1}},
		hooks:     &conf{PosRestart: []string{"somescript.sh"}},
	}
	w := new(bytes.Buffer)
	l := stdlog.New(w, "", stdlog.LstdFlags)
	log.SetLogger(l)
	err := a.preRestart(w)
	c.Assert(err, IsNil)
	st := strings.Split(w.String(), "\n")
	c.Assert(st[len(st)-2], Matches, ".*Skipping pre-restart hooks...")
}

func (s *S) TestPosRestart(c *C) {
	s.provisioner.PrepareOutput([]byte("restarted"))
	a := App{
		Name:      "something",
		Framework: "django",
		State:     string(provision.StatusStarted),
		hooks:     &conf{PosRestart: []string{"pos.sh"}},
	}
	w := new(bytes.Buffer)
	err := a.posRestart(w)
	c.Assert(err, IsNil)
	st := strings.Replace(w.String(), "\n", "###", -1)
	c.Assert(st, Matches, `.*restarted$`)
}

func (s *S) TestPosRestartWhenAppConfDoesNotExists(c *C) {
	a := App{Name: "something", Framework: "django"}
	w := new(bytes.Buffer)
	l := stdlog.New(w, "", stdlog.LstdFlags)
	log.SetLogger(l)
	err := a.posRestart(w)
	c.Assert(err, IsNil)
	st := strings.Split(w.String(), "\n")
	c.Assert(st[len(st)-2], Matches, ".*Skipping pos-restart hooks...")
}

func (s *S) TestSkipsPosRestartWhenPosRestartSectionDoesNotExists(c *C) {
	a := App{
		Name:      "something",
		Framework: "django",
		Units:     []Unit{{State: string(provision.StatusStarted), Machine: 1}},
		hooks:     &conf{PreRestart: []string{"somescript.sh"}},
	}
	w := new(bytes.Buffer)
	l := stdlog.New(w, "", stdlog.LstdFlags)
	log.SetLogger(l)
	err := a.posRestart(w)
	c.Assert(err, IsNil)
	st := strings.Split(w.String(), "\n")
	c.Assert(st[len(st)-2], Matches, ".*Skipping pos-restart hooks...")
}

func (s *S) TestInstallDeps(c *C) {
	s.provisioner.PrepareOutput([]byte("dependencies installed"))
	a := App{
		Name:      "someApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		State:     string(provision.StatusStarted),
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	var buf bytes.Buffer
	err = a.InstallDeps(&buf)
	c.Assert(err, IsNil)
	c.Assert(buf.String(), Equals, "dependencies installed")
	cmds := s.provisioner.GetCmds("/var/lib/tsuru/hooks/dependencies", &a)
	c.Assert(cmds, HasLen, 1)
}

func (s *S) TestRestart(c *C) {
	s.provisioner.PrepareOutput(nil) // loadHooks
	s.provisioner.PrepareOutput([]byte("nothing"))
	a := App{
		Name:      "someApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		State:     string(provision.StatusStarted),
	}
	var b bytes.Buffer
	err := a.Restart(&b)
	c.Assert(err, IsNil)
	result := strings.Replace(b.String(), "\n", "#", -1)
	c.Assert(result, Matches, ".*# ---> Restarting your app#.*")
	cmds := s.provisioner.GetCmds("/var/lib/tsuru/hooks/restart", &a)
	c.Assert(cmds, HasLen, 1)
}

func (s *S) TestRestartRunsPreRestartHook(c *C) {
	s.provisioner.PrepareOutput([]byte("pre-restart-by-restart"))
	s.provisioner.PrepareOutput([]byte("restart"))
	a := App{
		Name:      "someApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		State:     string(provision.StatusStarted),
		hooks:     &conf{PreRestart: []string{"pre.sh"}},
	}
	var buf bytes.Buffer
	err := a.Restart(&buf)
	c.Assert(err, IsNil)
	content := buf.String()
	content = strings.Replace(content, "\n", "###", -1)
	c.Assert(content, Matches, "^.*### ---> Running pre-restart###.*$")
}

func (s *S) TestRestartRunsPosRestartHook(c *C) {
	s.provisioner.PrepareOutput([]byte("pos-restart-by-restart"))
	s.provisioner.PrepareOutput([]byte("restart"))
	a := App{
		Name:      "someApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		State:     string(provision.StatusStarted),
		hooks:     &conf{PosRestart: []string{"pos.sh"}},
	}
	var buf bytes.Buffer
	err := a.Restart(&buf)
	c.Assert(err, IsNil)
	content := buf.String()
	content = strings.Replace(content, "\n", "###", -1)
	c.Assert(content, Matches, "^.*### ---> Running pos-restart###.*$")
}

func (s *S) TestLog(c *C) {
	a := App{Name: "newApp"}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = a.Log("last log msg", "tsuru")
	c.Assert(err, IsNil)
	var instance App
	err = db.Session.Apps().Find(bson.M{"name": a.Name}).One(&instance)
	logLen := len(instance.Logs)
	c.Assert(instance.Logs[logLen-1].Message, Equals, "last log msg")
	c.Assert(instance.Logs[logLen-1].Source, Equals, "tsuru")
}

func (s *S) TestLogShouldAddOneRecordByLine(c *C) {
	a := App{Name: "newApp"}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = a.Log("last log msg\nfirst log", "source")
	c.Assert(err, IsNil)
	instance := App{}
	err = db.Session.Apps().Find(bson.M{"name": a.Name}).One(&instance)
	logLen := len(instance.Logs)
	c.Assert(instance.Logs[logLen-2].Message, Equals, "last log msg")
	c.Assert(instance.Logs[logLen-1].Message, Equals, "first log")
}

func (s *S) TestLogShouldNotLogBlankLines(c *C) {
	a := App{
		Name: "newApp",
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = a.Log("some message", "tsuru")
	c.Assert(err, IsNil)
	err = a.Log("", "")
	c.Assert(err, IsNil)
	var instance App
	err = db.Session.Apps().Find(bson.M{"name": a.Name}).One(&instance)
	logLen := len(instance.Logs)
	c.Assert(instance.Logs[logLen-1].Message, Not(Equals), "")
}

func (s *S) TestGetTeams(c *C) {
	app := App{Name: "app", Teams: []string{s.team.Name}}
	teams := app.GetTeams()
	c.Assert(teams, HasLen, 1)
	c.Assert(teams[0].Name, Equals, s.team.Name)
}

func (s *S) TestSetTeams(c *C) {
	app := App{Name: "app"}
	app.SetTeams([]auth.Team{s.team})
	c.Assert(app.Teams, DeepEquals, []string{s.team.Name})
}

func (s *S) TestSetTeamsSortTeamNames(c *C) {
	app := App{Name: "app"}
	app.SetTeams([]auth.Team{s.team, {Name: "zzz"}, {Name: "aaa"}})
	c.Assert(app.Teams, DeepEquals, []string{"aaa", s.team.Name, "zzz"})
}

func (s *S) TestGetUnits(c *C) {
	app := App{Units: []Unit{{Ip: "1.1.1.1"}}}
	expected := []bind.Unit{bind.Unit(&Unit{Ip: "1.1.1.1", app: &app})}
	c.Assert(app.GetUnits(), DeepEquals, expected)
}

func (s *S) TestAppMarshalJson(c *C) {
	app := App{
		Name:      "Name",
		State:     "State",
		Framework: "Framework",
		Teams:     []string{"team1"},
	}
	expected := make(map[string]interface{})
	expected["Name"] = "Name"
	expected["State"] = "State"
	expected["Framework"] = "Framework"
	expected["Repository"] = repository.GetUrl(app.Name)
	expected["Teams"] = []interface{}{"team1"}
	expected["Units"] = interface{}(nil)
	data, err := app.MarshalJSON()
	c.Assert(err, IsNil)
	result := make(map[string]interface{})
	err = json.Unmarshal(data, &result)
	c.Assert(err, IsNil)
	c.Assert(result, DeepEquals, expected)
}

func (s *S) TestRun(c *C) {
	s.provisioner.PrepareOutput([]byte("a lot of files"))
	app := App{Name: "myapp", State: string(provision.StatusStarted)}
	var buf bytes.Buffer
	err := app.Run("ls -lh", &buf)
	c.Assert(err, IsNil)
	c.Assert(buf.String(), Equals, "a lot of files")
	expected := "[ -f /home/application/apprc ] && source /home/application/apprc;"
	expected += " [ -d /home/application/current ] && cd /home/application/current;"
	expected += " ls -lh"
	cmds := s.provisioner.GetCmds(expected, &app)
	c.Assert(cmds, HasLen, 1)
}

func (s *S) TestRunWithoutEnv(c *C) {
	s.provisioner.PrepareOutput([]byte("a lot of files"))
	app := App{Name: "myapp", State: string(provision.StatusStarted)}
	var buf bytes.Buffer
	err := app.run("ls -lh", &buf)
	c.Assert(err, IsNil)
	c.Assert(buf.String(), Equals, "a lot of files")
	cmds := s.provisioner.GetCmds("ls -lh", &app)
	c.Assert(cmds, HasLen, 1)
}

func (s *S) TestCommand(c *C) {
	s.provisioner.PrepareOutput([]byte("lots of files"))
	app := App{Name: "myapp", State: string(provision.StatusStarted)}
	var buf bytes.Buffer
	err := app.Command(&buf, &buf, "ls -lh")
	c.Assert(err, IsNil)
	c.Assert(buf.String(), Equals, "lots of files")
	cmds := s.provisioner.GetCmds("ls -lh", &app)
	c.Assert(cmds, HasLen, 1)
}

func (s *S) TestAppShouldBeARepositoryUnit(c *C) {
	var _ repository.Unit = &App{}
}

func (s *S) TestSerializeEnvVars(c *C) {
	s.provisioner.PrepareOutput([]byte("exported"))
	app := App{
		Name:  "time",
		Teams: []string{s.team.Name},
		State: string(provision.StatusStarted),
		Env: map[string]bind.EnvVar{
			"http_proxy": {
				Name:   "http_proxy",
				Value:  "http://theirproxy.com:3128/",
				Public: true,
			},
		},
	}
	err := app.SerializeEnvVars()
	c.Assert(err, IsNil)
	cmds := s.provisioner.GetCmds("", &app)
	c.Assert(cmds, HasLen, 1)
	cmdRegexp := `^cat > /home/application/apprc <<END # generated by tsuru .*`
	cmdRegexp += ` export http_proxy="http://theirproxy.com:3128/" END $`
	cmd := strings.Replace(cmds[0].Cmd, "\n", " ", -1)
	c.Assert(cmd, Matches, cmdRegexp)
}

func (s *S) TestSerializeEnvVarsErrorWithoutOutput(c *C) {
	app := App{
		Name: "intheend",
		Env: map[string]bind.EnvVar{
			"https_proxy": {
				Name:   "https_proxy",
				Value:  "https://secureproxy.com:3128/",
				Public: true,
			},
		},
	}
	err := app.SerializeEnvVars()
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `^Failed to write env vars: App must be started.*\.$`)
}

func (s *S) TestSerializeEnvVarsErrorWithOutput(c *C) {
	s.provisioner.PrepareOutput([]byte("This program has performed an illegal operation"))
	s.provisioner.PrepareFailure("ExecuteCommand", errors.New("exit status 1"))
	app := App{
		Name:  "intheend",
		State: string(provision.StatusStarted),
		Env: map[string]bind.EnvVar{
			"https_proxy": {
				Name:   "https_proxy",
				Value:  "https://secureproxy.com:3128/",
				Public: true,
			},
		},
	}
	err := app.SerializeEnvVars()
	c.Assert(err, NotNil)
	expected := "Failed to write env vars (exit status 1): This program has performed an illegal operation."
	c.Assert(err.Error(), Equals, expected)
}

func (s *S) TestListReturnsAppsForAGivenUser(c *C) {
	a := App{
		Name:  "testapp",
		Teams: []string{s.team.Name},
	}
	a2 := App{
		Name:  "othertestapp",
		Teams: []string{"commonteam", s.team.Name},
	}
	err := db.Session.Apps().Insert(&a)
	c.Assert(err, IsNil)
	err = db.Session.Apps().Insert(&a2)
	c.Assert(err, IsNil)
	defer func() {
		db.Session.Apps().Remove(bson.M{"name": a.Name})
		db.Session.Apps().Remove(bson.M{"name": a2.Name})
	}()
	apps, err := List(s.user)
	c.Assert(err, IsNil)
	c.Assert(len(apps), Equals, 2)
}

func (s *S) TestListReturnsEmptyAppArrayWhenUserHasNoAccessToAnyApp(c *C) {
	apps, err := List(s.user)
	c.Assert(err, IsNil)
	c.Assert(apps, DeepEquals, []App(nil))
}

func (s *S) TestListReturnsAllAppsWhenUserIsInAdminTeam(c *C) {
	a := App{Name: "testApp", Teams: []string{"notAdmin", "noSuperUser"}}
	err := db.Session.Apps().Insert(&a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	s.createAdminUserAndTeam(c)
	defer s.removeAdminUserAndTeam(c)
	apps, err := List(s.admin)
	c.Assert(len(apps), Greater, 0)
	c.Assert(apps[0].Name, Equals, "testApp")
	c.Assert(apps[0].Teams, DeepEquals, []string{"notAdmin", "noSuperUser"})
}

func (s *S) TestGetName(c *C) {
	a := App{Name: "something"}
	c.Assert(a.GetName(), Equals, a.Name)
}

func (s *S) TestGetFramework(c *C) {
	a := App{Framework: "django"}
	c.Assert(a.GetFramework(), Equals, a.Framework)
}

func (s *S) TestGetProvisionUnits(c *C) {
	a := App{Name: "anycolor", Units: []Unit{{Name: "i-0800"}, {Name: "i-0900"}, {Name: "i-a00"}}}
	gotUnits := a.ProvisionUnits()
	for i := range a.Units {
		if gotUnits[i].GetName() != a.Units[i].Name {
			c.Errorf("Failed at position %d: Want %q. Got %q.", i, a.Units[i].Name, gotUnits[i].GetName())
		}
	}
}
