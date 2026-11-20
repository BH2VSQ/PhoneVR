package main

import "os"

func netHostName() (string, error) { return os.Hostname() }
