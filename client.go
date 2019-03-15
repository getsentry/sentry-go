package sentry

import "math/rand"

const Version string = "0.0.0-beta"
const UserAgent string = "sentry.go/" + Version

type Event struct {
	message string
}

type Client struct {
	environment string
}

func NewClient() *Client {
	return &Client{
		environment: string(rand.Intn(1337)),
	}
}
