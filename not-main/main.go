package main

import (
)

type NotMain struct{}

func (m *NotMain) Hello() string {
	return "Hello, World!"
}
