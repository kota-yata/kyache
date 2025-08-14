package main

import (
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

type QLBCID struct {
	rotation uint8
	serverID uint16
	pad      uint32
}

// function to create 8-bit server ID from ip and port
func GenServerID(ip net.IP, port int) uint16 {
	serverID := uint16(0)
	if ip.To4() != nil {
		serverID = uint16(ip[2])<<8 | uint16(ip[3])
	} else {
		serverID = uint16(ip[12])<<8 | uint16(ip[13])
	}
	serverID = serverID<<8 | uint16(port&0xFF)
	serverID = serverID<<8 | uint16((port>>8)&0xFF)
	if serverID == 0 {
		serverID = 1
	}
	return serverID
}

func NewQLBCID(ip net.IP, port int) *QLBCID {
	rotation := uint8(0) // default
	serverID := GenServerID(ip, port)
	pad := uint32(0)
	return &QLBCID{
		rotation: rotation,
		serverID: serverID,
		pad:      pad,
	}
}

func (c *QLBCID) GenerateConnectionID() (quic.ConnectionID, error) {
	cid := make([]byte, 8)
	cid[0] = c.rotation
	cid[1] = byte(c.serverID >> 8)
	cid[2] = byte(c.serverID)
	cid[3] = byte(c.pad >> 24)
	cid[4] = byte(c.pad >> 16)
	cid[5] = byte(c.pad >> 8)
	cid[6] = byte(c.pad)
	cid[7] = byte(time.Now().UnixNano() & 0xFF) // last byte for uniqueness
	return quic.ConnectionIDFromBytes(cid), nil
}

func (c *QLBCID) ConnectionIDLen() int {
	return 8
}

var _ quic.ConnectionIDGenerator = &QLBCID{}
