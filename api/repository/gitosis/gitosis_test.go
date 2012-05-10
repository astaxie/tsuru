package gitosis

import (
	"fmt"
	ini "github.com/kless/goconfig/config"
	"github.com/timeredbull/tsuru/config"
	. "launchpad.net/gocheck"
	"os"
	"os/exec"
	"path"
)

func (s *S) TestAddProject(c *C) {
	err := AddGroup("someGroup")
	c.Assert(err, IsNil)
	err = AddProject("someGroup", "someProject")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(conf.HasOption("group someGroup", "writable"), Equals, true)
	obtained, err := conf.String("group someGroup", "writable")
	c.Assert(err, IsNil)
	c.Assert(obtained, Equals, "someProject")
	// try to add to an inexistent group
	err = AddProject("inexistentGroup", "someProject")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^Section group inexistentGroup doesn't exists$")
}

func (s *S) TestAddMoreThenOneProject(c *C) {
	err := AddGroup("fooGroup")
	c.Assert(err, IsNil)
	err = AddProject("fooGroup", "take-over-the-world")
	c.Assert(err, IsNil)
	err = AddProject("fooGroup", "someProject")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	obtained, err := conf.String("group fooGroup", "writable")
	c.Assert(err, IsNil)
	c.Assert(obtained, Equals, "take-over-the-world someProject")
}

func (s *S) TestAddProjectCommitAndPush(c *C) {
	err := AddGroup("myGroup")
	c.Assert(err, IsNil)
	err = AddProject("myGroup", "myProject")
	c.Assert(err, IsNil)
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	os.Chdir(s.gitosisBare)
	bareOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%s").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(pwd)
	expected := "Added project myProject to group myGroup"
	c.Assert(string(bareOutput), Equals, expected)
}

func (s *S) TestAppendToOption(c *C) {
	group := "fooGroup"
	section := fmt.Sprintf("group %s", group)
	err := AddGroup(group)
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	err = addOption(conf, section, "writable", "firstProject")
	c.Assert(err, IsNil)
	// Check if option were added
	obtained, err := conf.String(section, "writable")
	c.Assert(err, IsNil)
	c.Assert(obtained, Equals, "firstProject")
	// Add one more value to same section/option
	err = addOption(conf, section, "writable", "anotherProject")
	c.Assert(err, IsNil)
	// Check if the values were appended
	obtained, err = conf.String(section, "writable")
	c.Assert(err, IsNil)
	c.Assert(obtained, Equals, "firstProject anotherProject")
}

func (s *S) TestAddGroup(c *C) {
	err := AddGroup("someGroup")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	//ensures that project have been added to gitosis.conf
	c.Assert(conf.HasSection("group someGroup"), Equals, true)
	//ensures that file is not overriden when a new project is added
	err = AddGroup("someOtherGroup")
	c.Assert(err, IsNil)
	// it should have both sections
	conf, err = ini.ReadDefault(path.Join(s.gitRoot, "gitosis-admin/gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasSection("group someGroup"), Equals, true)
	c.Assert(conf.HasSection("group someOtherGroup"), Equals, true)
}

func (s *S) TestAddGroupShouldReturnErrorWhenSectionAlreadyExists(c *C) {
	err := AddGroup("aGroup")
	c.Assert(err, IsNil)
	err = AddGroup("aGroup")
	c.Assert(err, NotNil)
}

func (s *S) TestAddGroupShouldCommitAndPushChangesToGitosisBare(c *C) {
	err := AddGroup("gandalf")
	c.Assert(err, IsNil)
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	os.Chdir(s.gitosisBare)
	bareOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%H").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(s.gitosisRepo)
	repoOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%H").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(pwd)
	c.Assert(string(repoOutput), Equals, string(bareOutput))
}

func (s *S) TestRemoveGroup(c *C) {
	err := AddGroup("someGroup")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasSection("group someGroup"), Equals, true)
	err = RemoveGroup("someGroup")
	conf, err = ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasSection("group someGroup"), Equals, false)
	pwd, err := os.Getwd()
	os.Chdir(s.gitosisBare)
	bareOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%s").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(pwd)
	expected := "Removing group someGroup from gitosis.conf"
	c.Assert(string(bareOutput), Equals, expected)
}

func (s *S) TestRemoveGroupCommitAndPushesChanges(c *C) {
	err := AddGroup("testGroup")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasSection("group testGroup"), Equals, true)
	err = RemoveGroup("testGroup")
	conf, err = ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasSection("group testGroup"), Equals, false)
}

func (s *S) TestAddMemberToGroup(c *C) {
	err := AddGroup("take-over-the-world")
	c.Assert(err, IsNil)
	err = addMember("take-over-the-world", "brain")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasSection("group take-over-the-world"), Equals, true)
	c.Assert(conf.HasOption("group take-over-the-world", "members"), Equals, true)
	members, err := conf.String("group take-over-the-world", "members")
	c.Assert(err, IsNil)
	c.Assert(members, Equals, "brain")
}

func (s *S) TestAddMemberToGroupCommitsAndPush(c *C) {
	err := AddGroup("someTeam")
	c.Assert(err, IsNil)
	err = addMember("someTeam", "brain")
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	os.Chdir(s.gitosisBare)
	bareOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%s").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(pwd)
	commitMsg := "Adding member brain to group someTeam"
	c.Assert(string(bareOutput), Equals, commitMsg)
}

func (s *S) TestAddTwoMembersToGroup(c *C) {
	err := AddGroup("pink-floyd")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "one-of-these-days")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "comfortably-numb")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	members, err := conf.String("group pink-floyd", "members")
	c.Assert(err, IsNil)
	c.Assert(members, Equals, "one-of-these-days comfortably-numb")
}

func (s *S) TestAddMemberToGroupReturnsErrorIfTheMemberIsAlreadyInTheGroup(c *C) {
	err := AddGroup("pink-floyd")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "time")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "time")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^Value time for option members in section group pink-floyd has already been added$")
}

func (s *S) TestAddMemberToAGroupThatDoesNotExistReturnError(c *C) {
	err := addMember("pink-floyd", "one-of-these-days")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^Group not found$")
}

func (s *S) TestRemoveMemberFromGroup(c *C) {
	err := AddGroup("pink-floyd")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "fat-old-sun")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "summer-68")
	c.Assert(err, IsNil)
	err = removeMember("pink-floyd", "fat-old-sun")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	option, err := conf.String("group pink-floyd", "members")
	c.Assert(err, IsNil)
	c.Assert(option, Equals, "summer-68")
}

func (s *S) TestRemoveMemberFromGroupCommitsAndPush(c *C) {
	err := AddGroup("pink-floyd")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "if")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "atom-heart-mother-suite")
	c.Assert(err, IsNil)
	err = removeMember("pink-floyd", "if")
	c.Assert(err, IsNil)
	os.Chdir(s.gitosisBare)
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	bareOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%s").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(pwd)
	commitMsg := "Removing member if from group pink-floyd"
	c.Assert(string(bareOutput), Equals, commitMsg)
}

func (s *S) TestRemoveMemberFromGroupRemovesTheOptionFromTheSectionWhenTheMemberIsTheLast(c *C) {
	err := AddGroup("pink-floyd")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "pigs-on-the-wing")
	c.Assert(err, IsNil)
	err = removeMember("pink-floyd", "pigs-on-the-wing")
	c.Assert(err, IsNil)
	conf, err := ini.ReadDefault(path.Join(s.gitosisRepo, "gitosis.conf"))
	c.Assert(err, IsNil)
	c.Assert(conf.HasOption("group pink-floyd", "members"), Equals, false)
}

func (s *S) TestRemoveMemberFromGroupReturnsErrorsIfTheGroupDoesNotContainTheGivenMember(c *C) {
	err := AddGroup("pink-floyd")
	c.Assert(err, IsNil)
	err = addMember("pink-floyd", "another-brick")
	c.Assert(err, IsNil)
	err = removeMember("pink-floyd", "pigs-on-the-wing")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^This group does not have this member$")
}

func (s *S) TestRemoveMemberFromGroupReturnsErrorIfTheGroupDoesNotExist(c *C) {
	err := removeMember("pink-floyd", "pigs-on-the-wing")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^Group not found$")
}

func (s *S) TestRemoveMemberFromGroupReturnsErrorsIfTheGroupDoesNotHaveAnyMember(c *C) {
	err := AddGroup("pato-fu")
	c.Assert(err, IsNil)
	err = removeMember("pato-fu", "eu-sei")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "^This group does not have any members$")
}

func (s *S) TestAddAndCommit(c *C) {
	confPath := path.Join(s.gitosisRepo, "gitosis.conf")
	conf, err := ini.ReadDefault(confPath)
	c.Assert(err, IsNil)
	conf.AddSection("foo bar")
	pushToGitosis("Some commit message")
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	os.Chdir(s.gitosisBare)
	bareOutput, err := exec.Command("git", "log", "-1", "--pretty=format:%s").CombinedOutput()
	c.Assert(err, IsNil)
	os.Chdir(pwd)
	c.Assert(string(bareOutput), Equals, "Some commit message")
}

func (s *S) TestConfPathReturnsGitosisConfPath(c *C) {
	repoPath, err := config.GetString("git:gitosis-repo")
	expected := path.Join(repoPath, "gitosis.conf")
	obtained, err := ConfPath()
	c.Assert(err, IsNil)
	c.Assert(obtained, Equals, expected)
}