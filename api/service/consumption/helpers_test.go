package consumption

import (
	. "github.com/timeredbull/tsuru/api/service"
	. "launchpad.net/gocheck"
)

func (s *S) TestGetServiceOrError(c *C) {
	srv := Service{Name: "foo", Teams: []string{s.team.Name}}
	err := srv.Create()
	c.Assert(err, IsNil)
	rSrv, err := GetServiceOrError("foo", s.user)
	c.Assert(err, IsNil)
	c.Assert(rSrv.Name, Equals, srv.Name)
}

func (s *S) TestGetServiceOrErrorShouldReturnErrorWhenUserHaveNoAccessToService(c *C) {
	srv := Service{Name: "foo"}
	err := srv.Create()
	c.Assert(err, IsNil)
	_, err = GetServiceOrError("foo", s.user)
	c.Assert(err, ErrorMatches, "^This user does not have access to this service$")
}

func (s *S) TestGetServiceOr404(c *C) {
	_, err := GetServiceOr404("foo")
	c.Assert(err, ErrorMatches, "^Service not found$")
}