package types

import "strings"

type DatabaseTarget struct {
	Name         string `json:"name"`
	Type         DBType `json:"type"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"db_name"`
	JobCount     int    `json:"job_count"`
}

func ParseDBType(s string) (DBType, bool) {
	switch strings.ToLower(s) {
	case "mysql":
		return MySQL, true
	case "mariadb":
		return MariaDB, true
	case "postgres", "postgresql":
		return Postgres, true
	default:
		return "", false
	}
}

type DBType string

const (
	MySQL    DBType = "mysql"
	MariaDB  DBType = "mariadb"
	Postgres DBType = "postgres"
)
