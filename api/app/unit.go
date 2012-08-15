package app

import (
	"fmt"
	"github.com/timeredbull/tsuru/log"
	"os/exec"
	"strconv"
	"strings"
)

type Unit struct {
	Type              string
	Name              string
	Machine           int
	Ip                string
	AgentState        string `yaml:"agent-state"`
	MachineAgentState string
	InstanceState     string
	InstanceId        string
	app               *App
}

func (u *Unit) Destroy() ([]byte, error) {
	cmd := exec.Command("juju", "destroy-service", "-e", u.app.JujuEnv, u.Name)
	log.Printf("destroying %s with name %s", u.Type, u.Name)
	out, err := cmd.CombinedOutput()
	log.Printf(string(out))
	if err != nil {
		return out, err
	}
	cmd = exec.Command("juju", "terminate-machine", "-e", u.app.JujuEnv, strconv.Itoa(u.Machine))
	return cmd.CombinedOutput()
}

func (u *Unit) Command(cmds ...string) ([]byte, error) {
	c := exec.Command("juju", "ssh", "-o", "StrictHostKeyChecking no", "-e", u.app.JujuEnv, strconv.Itoa(u.Machine))
	c.Args = append(c.Args, cmds...)
	log.Printf("executing %s on %s", strings.Join(cmds, " "), u.Name)
	return c.CombinedOutput()
}

func (u *Unit) GetName() string {
	return u.Name
}

func (u *Unit) GetIp() string {
	return u.Ip
}

func (u *Unit) ExecuteHook(hook string) ([]byte, error) {
	cmd := fmt.Sprintf("/var/lib/tsuru/hooks/%s", hook)
	output, err := u.Command(cmd)
	log.Print(string(output))
	return output, err
}