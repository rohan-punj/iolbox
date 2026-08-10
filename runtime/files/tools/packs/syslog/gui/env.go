package main

import "os"

func envLookup(key string) string { return os.Getenv(key) }
