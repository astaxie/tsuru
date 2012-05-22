package repository

import (
	"fmt"
	"github.com/timeredbull/tsuru/api/unit"
	"github.com/timeredbull/tsuru/log"
)

const gitServer = "tsuru.plataformas.glb.com"

func Clone(app string, machine int) (err error) {
	u := unit.Unit{Name: app, Machine: machine}
	cmd := fmt.Sprintf(`"git clone %s /home/application/%s"`, GetReadOnlyUrl(app), app)
	output, err := u.Command(cmd)
	log.Printf("Command output: " + string(output))
	if err != nil {
		return
	}
	return
}

func GetUrl(app string) string {
	return fmt.Sprintf("git@%s:%s.git", gitServer, app)
}

func GetReadOnlyUrl(app string) string {
	return fmt.Sprintf("git://%s/%s.git", gitServer, app)
}