// Package vulntest2 contains fixed code demonstrating secure patterns for TEST-2 rerun.
package vulntest2

import (
	"html/template"
	"net/http"
	"os/exec"
)

var outputTmpl = template.Must(template.New("cmd").Parse("{{.}}"))

// RunUserCommand executes a statically allowlisted command and renders output via html/template.
func RunUserCommand(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("cmd")
	var cmd *exec.Cmd
	switch name {
	case "date":
		cmd = exec.Command("date")
	case "uptime":
		cmd = exec.Command("uptime")
	default:
		http.Error(w, "unknown command", http.StatusBadRequest)
		return
	}
	out, err := cmd.Output()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = outputTmpl.Execute(w, string(out))
}
