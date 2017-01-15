// Copyright (c) 2016 Jeevanandam M (https://github.com/jeevatkm)
// go-aah/tools source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"text/template"
)

// Command structure insprired by `go` command
type command struct {
	// Name of the command
	Name string

	// UsageLine is the one-line usage message.
	UsageLine string

	// Total no of arguments (mandatory & optionals)
	ArgsCount int

	// Short is the short description shown in the 'aah help' output.
	Short string

	// Long is the long message shown in the 'aah help <this-command>' output.
	Long string

	// Run runs the command.
	// The args are the arguments after the command name.
	Run func(args []string)
}

// Usage displays the usage line and long description then exits
func (c *command) Usage() {
	fmt.Fprintf(os.Stderr, "Usage: %v\n\n", c.UsageLine)
	fmt.Fprintf(os.Stderr, "%v\n\n", strings.TrimSpace(c.Long))
	os.Exit(2)
}

// Commands groups set of commands together and provides handy methods around it
type commands []*command

// Find finds the command from command name otherwise returns error
func (c *commands) Find(name string) (*command, error) {
	for _, cmd := range *c {
		if cmd.Name == name {
			return cmd, nil
		}
	}
	return nil, fmt.Errorf("command %v not found", name)
}

// Helper methods for commands

func displayUsage(exitCode int, text string, data interface{}) {
	renderTmpl(os.Stdout, text, data)
	os.Exit(exitCode)
}

func commandNotFound(name string) {
	fmt.Printf("Command '%v' is not found, available commands and it's usage.\n\n", name)
	displayUsage(2, usageTemplate, cliCommands)
}

func renderTmpl(w io.Writer, text string, data interface{}) {
	tmpl := template.Must(template.New("").Parse(text))
	if err := tmpl.Execute(w, data); err != nil {
		log.Fatalf("Unable to render template text: %s", err)
	}
}
