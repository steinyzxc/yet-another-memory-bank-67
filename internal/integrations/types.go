package integrations

type Event struct {
	Agent             string
	ExternalSessionID string
	CWD               string
	Kind              string
	Tool              string
	PayloadJSON       []byte
}
