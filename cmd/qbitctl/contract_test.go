package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func TestCommandTreeHasDescriptionsAndKnownGroups(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]bool, len(root.Groups()))
	for _, group := range root.Groups() {
		groups[group.ID] = true
	}
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd != root && strings.TrimSpace(cmd.Short) == "" {
			t.Errorf("command %q has no Short description", cmd.CommandPath())
		}
		if cmd.GroupID != "" && !groups[cmd.GroupID] {
			t.Errorf("command %q uses unknown help group %q", cmd.CommandPath(), cmd.GroupID)
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func TestJSONOutputModesAreMutuallyExclusiveBeforeLogin(t *testing.T) {
	const fullHash = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name string
		args []string
	}{
		{name: "info", args: []string{"info", "--json", "--server-json"}},
		{name: "list", args: []string{"list", "--json", "--server-json"}},
		{name: "show", args: []string{"show", fullHash, "--json", "--server-json"}},
		{name: "get", args: []string{"get", "name", fullHash, "--json", "--server-json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newQBTestServer(t)
			withQBServerClientFactory(t, server)
			root := newRootCmd()
			root.SetArgs(append([]string{"--url", server.creds().URL, "--user", "admin", "--pass", "secret"}, tc.args...))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			err := root.Execute()
			var coded *qbt.CodedError
			if !errors.As(err, &coded) || coded.Code != qbt.ExitBadArgs {
				t.Fatalf("error = %v, want ExitBadArgs", err)
			}
			if server.LoginCalls != 0 {
				t.Fatalf("conflicting output modes authenticated: %d logins", server.LoginCalls)
			}
		})
	}
}

func TestOutputModesDeclareTheirConflictRules(t *testing.T) {
	const fullHash = "0123456789abcdef0123456789abcdef01234567"
	for _, name := range []string{"list", "show"} {
		for _, pair := range [][2]string{{"json", "template"}, {"server-json", "fields"}, {"template", "fields"}} {
			t.Run(name+"/"+pair[0]+"-"+pair[1], func(t *testing.T) {
				root := newRootCmd()
				cmd, _, err := root.Find([]string{name})
				if err != nil {
					t.Fatal(err)
				}
				for _, flag := range pair {
					value := "true"
					if flag == "fields" {
						value = "name"
					}
					if err := cmd.Flags().Set(flag, value); err != nil {
						t.Fatalf("set --%s: %v", flag, err)
					}
				}
				args := []string(nil)
				if name == "show" {
					args = []string{fullHash}
				}
				if err := cmd.Args(cmd, args); err == nil {
					t.Fatalf("accepted conflicting output flags %v", pair)
				}
			})
		}
	}
}

func TestAuthenticatedCommandsRejectBadArgumentsBeforeLogin(t *testing.T) {
	root := newRootCmd()
	badArgs := map[string][]string{
		"info":        {"unexpected"},
		"add":         nil,
		"list":        {"unexpected"},
		"search":      nil,
		"show":        nil,
		"get":         nil,
		"set":         nil,
		"start":       nil,
		"pause":       nil,
		"force-start": nil,
		"move":        nil,
		"remove":      nil,
		"delete":      nil,
	}
	for name, args := range badArgs {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if cmd.RunE == nil {
			t.Fatalf("authenticated command %s has no RunE", name)
		}
		if err := cmd.Args(cmd, args); err == nil {
			t.Errorf("%s accepted invalid arguments %v", name, args)
		}
	}
}

func TestREADMECommandsTableMatchesRoot(t *testing.T) {
	readme := readRepositoryREADME(t)
	root := newRootCmd()
	commands := make(map[string]bool)
	inTable := false
	for _, line := range strings.Split(readme, "\n") {
		if line == "| Command | What it does |" {
			inTable = true
			continue
		}
		if inTable && !strings.HasPrefix(line, "|") {
			break
		}
		if !inTable || strings.HasPrefix(line, "| ---") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) < 2 {
			continue
		}
		for _, match := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(columns[1], -1) {
			for _, name := range strings.Split(match[1], ",") {
				commands[strings.TrimSpace(name)] = true
			}
		}
	}
	if len(commands) == 0 {
		t.Fatal("README command table was not found")
	}
	for name := range commands {
		if _, _, err := root.Find([]string{name}); err != nil {
			t.Errorf("README command %q does not resolve: %v", name, err)
		}
	}
}

func TestREADMEEnvironmentVariablesHaveRuntimeConsumers(t *testing.T) {
	readme := readRepositoryREADME(t)
	varPattern := regexp.MustCompile(`QBITCTL_[A-Z0-9_]+`)
	wanted := make(map[string]bool)
	for _, name := range varPattern.FindAllString(readme, -1) {
		wanted[name] = true
	}

	runtimeText := strings.Builder{}
	repositoryRoot := filepath.Join("..", "..")
	for _, root := range []string{"cmd", "pkg"} {
		root = filepath.Join(repositoryRoot, root)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			runtimeText.Write(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(repositoryRoot, "scripts", "install.sh")); err == nil {
		runtimeText.Write(data)
	} else {
		t.Fatal(err)
	}
	for name := range wanted {
		if name == "QBITCTL_TEST_KEYCHAIN" {
			continue
		}
		if !strings.Contains(runtimeText.String(), name) {
			t.Errorf("README documents %s without a runtime consumer", name)
		}
	}
}

func TestREADMECommandExamplesResolveAndUseDeclaredFlags(t *testing.T) {
	readme := readRepositoryREADME(t)
	root := newRootCmd()
	inFence := false
	for lineNumber, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence || !strings.Contains(line, "qbitctl") {
			continue
		}
		index := strings.Index(line, "qbitctl")
		if index > 0 {
			before := line[index-1]
			if (before >= 'A' && before <= 'Z') || (before >= 'a' && before <= 'z') || before == '_' {
				continue
			}
		}
		commandLine := strings.TrimSpace(line[index:])
		commandLine = strings.SplitN(commandLine, " #", 2)[0]
		commandLine = strings.TrimSpace(commandLine)
		if strings.HasPrefix(commandLine, "qbitctl") {
			if err := validateREADMEInvocation(root, commandLine); err != nil {
				t.Fatalf("README line %d: %v\n%s", lineNumber+1, err, commandLine)
			}
		}
	}
}

func validateREADMEInvocation(root *cobra.Command, line string) error {
	tokens := strings.Fields(line)
	if len(tokens) < 2 || tokens[0] != "qbitctl" {
		return nil
	}
	commandIndex := -1
	var command *cobra.Command
	for index := 1; index < len(tokens); index++ {
		if strings.HasPrefix(tokens[index], "-") {
			continue
		}
		candidate, _, err := root.Find([]string{tokens[index]})
		if err != nil {
			continue
		}
		commandIndex, command = index, candidate
		break
	}
	if command == nil {
		return errors.New("no command name found")
	}
	if err := validateREADMEFlags(root, tokens[1:commandIndex]); err != nil {
		return fmt.Errorf("root flags: %w", err)
	}
	path := []string{tokens[commandIndex]}
	if commandIndex+1 < len(tokens) {
		if child, _, err := root.Find([]string{tokens[commandIndex], tokens[commandIndex+1]}); err == nil {
			path = append(path, tokens[commandIndex+1])
			command = child
			commandIndex++
		}
	}
	if err := validateREADMEFlags(command, tokens[commandIndex+1:]); err != nil {
		return fmt.Errorf("%s flags: %w", strings.Join(path, " "), err)
	}
	return nil
}

func validateREADMEFlags(command *cobra.Command, tokens []string) error {
	for index := 0; index < len(tokens); index++ {
		token := strings.Trim(tokens[index], "'\";,)")
		if token == "--" || !strings.HasPrefix(token, "-") || token == "-" {
			continue
		}
		name := strings.TrimLeft(token, "-")
		if equals := strings.IndexByte(name, '='); equals >= 0 {
			name = name[:equals]
		}
		flag := command.Flag(name)
		if flag == nil && len(name) == 1 {
			flag = command.Flags().ShorthandLookup(name)
		}
		if flag == nil {
			return errors.New("undeclared flag " + token)
		}
		if !strings.Contains(token, "=") && flag.NoOptDefVal == "" && index+1 < len(tokens) {
			index++
		}
	}
	return nil
}

func readRepositoryREADME(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}
