package types

type DatabaseJob struct {
	ID                    string   `json:"id"`
	Store                 string   `config:"type=string,required" json:"store"`
	Target                string   `config:"type=string,required" json:"target"`
	TargetHost            string   `json:"target_host"`
	Schedule              string   `config:"type=string" json:"schedule"`
	Comment               string   `config:"type=string" json:"comment"`
	NotificationMode      string   `config:"key=notification_mode,type=string" json:"notification-mode"`
	Namespace             string   `config:"type=string" json:"ns"`
	NextRun               int64    `json:"next-run"`
	Retry                 int      `config:"type=int" json:"retry"`
	RetryInterval         int      `config:"type=int" json:"retry-interval"`
	CurrentPID            int      `config:"key=current_pid,type=int" json:"current_pid"`
	LastRunUpid           string   `config:"key=last_run_upid,type=string" json:"last-run-upid"`
	LastRunState          string   `json:"last-run-state"`
	LastRunEndtime        int64    `json:"last-run-endtime"`
	LastSuccessfulEndtime int64    `json:"last-successful-endtime"`
	LastSuccessfulUpid    string   `config:"key=last_successful_upid,type=string" json:"last-successful-upid"`
	Duration              int64    `json:"duration"`
	ExpectedSize          int      `json:"expected_size,omitempty"`
	UPIDs                 []string `json:"upids"`
}
