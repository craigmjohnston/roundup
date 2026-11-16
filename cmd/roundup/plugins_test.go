package main

import (
	"os/exec"
	"strings"
	"testing"
)

const pluginsPath = "github.com/craigmjohnston/roundup/plugins/"

func TestAppDependsOnNoPlugin(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if strings.HasPrefix(pkg+"/", pluginsPath) {
			t.Errorf("the app imports %s; plugins are separate programs that speak the protocol", pkg)
		}
	}
}
