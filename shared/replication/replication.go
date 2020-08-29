package replication

import "fmt"

type Log struct {
	Operation  Operation `json:"operation"`
	User       string    `json:"user"`
	Repo       string    `json:"repo"`
	TargetNode string    `json:"target_node"`
	RemoteNode string    `json:"remote_node"`
	Cluster    string    `json:"cluster"`
}

type Operation string

const (
	CreateRepo Operation = "CreateRepo"
	UpdateRepo Operation = "UpdateRepo"
)

const LogQueueName = "replication_log"

type GroupID string

func NewGroupID(addr, usr, repo string) GroupID {
	return GroupID(fmt.Sprintf("%s:%s:%s", addr, usr, repo))
}
