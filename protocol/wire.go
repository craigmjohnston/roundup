// Package protocol holds the message shapes of PROTOCOL.md, shared by the app's runner and by plugins written in Go.
package protocol

import "github.com/craigmjohnston/roundup/task"

// Version is the protocol number a hello carries.
const Version = 1

// Hello is the first line a program writes.
type Hello struct {
	Type     string     `json:"type"`
	Protocol int        `json:"protocol"`
	Source   SourceInfo `json:"source"`
}

// SourceInfo is Hello's description of the source.
type SourceInfo struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Icon      string          `json:"icon"`
	Icons     map[string]Icon `json:"icons"`
	Sections  []task.Section  `json:"sections"`
	CanSignIn bool            `json:"canSignIn"`
}

// Icon is an SVG in a hello.
type Icon struct {
	SVG      string `json:"svg"`
	Template bool   `json:"template"`
}

// NewHello describes info to the app.
func NewHello(info task.Info, canSignIn bool) Hello {
	src := SourceInfo{ID: info.ID, Name: info.Name, Icon: info.Icon, Sections: info.Sections, CanSignIn: canSignIn}
	if len(info.Icons) > 0 {
		src.Icons = map[string]Icon{}
	}
	for key, ic := range info.Icons {
		src.Icons[key] = Icon{SVG: string(ic.SVG), Template: ic.Template}
	}
	return Hello{Type: "hello", Protocol: Version, Source: src}
}

// Info is the task.Info the hello describes; Name defaults to the ID.
func (i SourceInfo) Info() task.Info {
	info := task.Info{ID: i.ID, Name: i.Name, Icon: i.Icon, Sections: i.Sections}
	if info.Name == "" {
		info.Name = i.ID
	}
	if len(i.Icons) > 0 {
		info.Icons = map[string]task.Icon{}
	}
	for key, ic := range i.Icons {
		info.Icons[key] = task.Icon{SVG: []byte(ic.SVG), Template: ic.Template}
	}
	return info
}
