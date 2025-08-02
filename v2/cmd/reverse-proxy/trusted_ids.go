package main

import (
	"bufio"
	"os"
	"github.com/syncthing/syncthing/lib/protocol"
)

func LoadTrustedDeviceIDs(path string) ([]protocol.DeviceID, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	trustedIds := []protocol.DeviceID{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 64 {
			line = line[:64]
		}
		id, err := protocol.DeviceIDFromString(line)
		if err != nil {
			return nil, err
		}
		trustedIds = append(trustedIds, id)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return trustedIds, nil
}
