package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/footprintai/containarium/internal/connectcore"
	"github.com/footprintai/containarium/internal/sshkey"
)

// handleConnect is the agent-native half of `containarium connect` — the
// thin MCP wrapper over the same connectcore resolve+authorize core the
// CLI verb uses (the "one Go func, two surfaces" pattern, per CLAUDE.md).
//
// An MCP call is request/response with no PTY, so only the two
// non-interactive modes are surfaced:
//
//   - config mode (no `exec` arg): resolve the box, authorize the managed
//     key, and return the ready `ssh <user>@<host>` invocation. The
//     human's terminal connects.
//   - exec mode (`exec` arg): run one command over SSH and return its
//     stdout / stderr / exit_code — operate the box without a TTY.
//
// Interactive (PTY) stays CLI-only.
func handleConnect(client *Client, args map[string]interface{}) (string, error) {
	box := strings.TrimSpace(getStringArg(args, "box", ""))
	if box == "" {
		return "", fmt.Errorf("`box` is required")
	}
	execCmd := getStringArg(args, "exec", "")
	userOverride := getStringArg(args, "user", "")
	hostOverride := getStringArg(args, "host", "")

	c, err := mcpGetContainer(client, box)
	if err != nil {
		return "", err
	}
	if !connectcore.IsRunning(c.State) {
		return "", fmt.Errorf("box %q is %s, not running — start it first", box, connectcore.PrettyState(c.State))
	}
	target, err := connectcore.BuildTarget(c, userOverride, hostOverride, 22)
	if err != nil {
		return "", err
	}

	// Reuse (or generate once) the managed key the `ssh setup` flow uses,
	// so the operator never hand-manages a key. The MCP server runs on the
	// operator's machine, so this is the same key material the CLI sees.
	pubPath, pub, _, err := sshkey.LocateOrGenerate(sshkey.LocateOpts{})
	if err != nil {
		return "", fmt.Errorf("locate or generate managed key: %w", err)
	}
	privPath := strings.TrimSuffix(pubPath, ".pub")

	if err := mcpAuthorizeKey(client, box, pub); err != nil {
		return "", fmt.Errorf("authorize key on %q: %w", box, err)
	}

	sshArgs := connectcore.BuildSSHArgs(target, privPath, execCmd)

	if execCmd == "" {
		// Config mode: hand the ready invocation back for the human to run.
		fp, _ := sshkey.Fingerprint(pub)
		return fmt.Sprintf(
			"✓ %s is ready — key %s authorized.\nRun this in your terminal:\n\n    ssh %s\n",
			box, fp, strings.Join(sshArgs, " ")), nil
	}
	// Exec mode: run the one-shot command and return its output + exit code.
	return runMCPSSHExec(sshArgs)
}

// runMCPSSHExec runs ssh non-interactively, capturing stdout and stderr
// separately, and returns a structured result. A non-zero remote exit is
// NOT a tool error — the command ran; the agent needs the output and the
// code. Only a failure to launch ssh is an error.
func runMCPSSHExec(sshArgs []string) (string, error) {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return "", fmt.Errorf("ssh not found in PATH: %w", err)
	}
	cmd := exec.Command(sshBin, sshArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if runErr := cmd.Run(); runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return "", fmt.Errorf("ssh: %w", runErr)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n", exitCode)
	if stdout.Len() > 0 {
		fmt.Fprintf(&b, "\n--- stdout ---\n%s", stdout.String())
	}
	if stderr.Len() > 0 {
		fmt.Fprintf(&b, "\n--- stderr ---\n%s", stderr.String())
	}
	return b.String(), nil
}

// mcpGetContainer GETs the box over the MCP client's daemon connection and
// decodes it into the shared connectcore DTO. doRequest folds the status
// into its error string; we detect 404 there to give a clean "not found".
func mcpGetContainer(client *Client, box string) (*connectcore.Container, error) {
	body, err := client.doRequest("GET", "/v1/containers/"+url.PathEscape(box), nil)
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, fmt.Errorf("box %q not found", box)
		}
		return nil, err
	}
	var resp connectcore.GetContainerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode container: %w", err)
	}
	return &resp.Container, nil
}

func mcpAuthorizeKey(client *Client, box, pub string) error {
	_, err := client.doRequest("POST",
		"/v1/containers/"+url.PathEscape(box)+"/ssh-keys",
		connectcore.AuthorizeKeyRequest{SshPublicKey: pub})
	return err
}
