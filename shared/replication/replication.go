package replication

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
