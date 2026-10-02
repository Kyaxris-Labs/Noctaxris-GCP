package cloudsql

import (
	"strings"
	"testing"
)

func TestNestedCreateDropUserAndDatabaseCmds(t *testing.T) {
	mysqlCreate := nestedCreateUserCmd("MYSQL_8_0", "appuser", "%", "s3cret")
	if len(mysqlCreate) == 0 || mysqlCreate[0] != "mysql" {
		t.Fatalf("mysql create=%#v", mysqlCreate)
	}
	if !strings.Contains(mysqlCreate[len(mysqlCreate)-1], "CREATE USER") {
		t.Fatalf("mysql create sql=%#v", mysqlCreate)
	}
	pgCreate := nestedCreateUserCmd("POSTGRES_16", "appuser", "", "s3cret")
	if len(pgCreate) == 0 || pgCreate[0] != "psql" {
		t.Fatalf("pg create=%#v", pgCreate)
	}
	if nestedCreateUserCmd("MYSQL_8_0", "bad;name", "%", "x") != nil {
		t.Fatal("unsafe user name")
	}
	if nestedCreateUserCmd("MYSQL_8_0", "ok", "bad;host", "x") != nil {
		t.Fatal("unsafe host")
	}
	emptyHost := nestedCreateUserCmd("MYSQL_8_0", "ok", "", "x")
	if len(emptyHost) == 0 || !strings.Contains(emptyHost[len(emptyHost)-1], "'%'") {
		t.Fatalf("default host=%#v", emptyHost)
	}

	mysqlDrop := nestedDropUserCmd("MYSQL_8_0", "appuser", "localhost")
	if len(mysqlDrop) == 0 || !strings.Contains(mysqlDrop[len(mysqlDrop)-1], "DROP USER") {
		t.Fatalf("mysql drop=%#v", mysqlDrop)
	}
	pgDrop := nestedDropUserCmd("POSTGRES_16", "appuser", "")
	if len(pgDrop) == 0 || pgDrop[0] != "psql" {
		t.Fatalf("pg drop=%#v", pgDrop)
	}
	if nestedDropUserCmd("MYSQL_8_0", "bad name", "%") != nil {
		t.Fatal("unsafe drop name")
	}
	if nestedDropUserCmd("MYSQL_8_0", "ok", "bad host") != nil {
		t.Fatal("unsafe drop host")
	}

	mysqlDB := nestedCreateDatabaseCmd("MYSQL_8_0", "appdb", "utf8mb4", "utf8mb4_unicode_ci")
	if len(mysqlDB) == 0 || !strings.Contains(mysqlDB[len(mysqlDB)-1], "CREATE DATABASE") {
		t.Fatalf("mysql db=%#v", mysqlDB)
	}
	if !strings.Contains(mysqlDB[len(mysqlDB)-1], "CHARACTER SET utf8mb4") {
		t.Fatalf("charset missing %#v", mysqlDB)
	}
	pgDB := nestedCreateDatabaseCmd("POSTGRES_16", "appdb", "", "")
	if len(pgDB) == 0 || pgDB[0] != "psql" {
		t.Fatalf("pg db=%#v", pgDB)
	}
	if nestedCreateDatabaseCmd("MYSQL_8_0", "bad;db", "", "") != nil {
		t.Fatal("unsafe db name")
	}

	mysqlDropDB := nestedDropDatabaseCmd("MYSQL_8_0", "appdb")
	if len(mysqlDropDB) == 0 || !strings.Contains(mysqlDropDB[len(mysqlDropDB)-1], "DROP DATABASE") {
		t.Fatalf("mysql drop db=%#v", mysqlDropDB)
	}
	pgDropDB := nestedDropDatabaseCmd("POSTGRES_16", "appdb")
	if len(pgDropDB) == 0 || pgDropDB[0] != "psql" {
		t.Fatalf("pg drop db=%#v", pgDropDB)
	}
	if nestedDropDatabaseCmd("MYSQL_8_0", "bad;db") != nil {
		t.Fatal("unsafe drop db")
	}

	cs, col := defaultCharsetCollation("MYSQL_8_4")
	if cs != "utf8mb4" || col != "utf8mb4_unicode_ci" {
		t.Fatalf("mysql defaults %s %s", cs, col)
	}
	cs, col = defaultCharsetCollation("POSTGRES_15")
	if cs != "UTF8" || col != "en_US.UTF8" {
		t.Fatalf("pg defaults %s %s", cs, col)
	}

	img, env, port := nestedImageEnv("MYSQL_8_0")
	if img == "" || len(env) == 0 || port != 3306 {
		t.Fatalf("mysql image env=%q %#v %d", img, env, port)
	}
	img, env, port = nestedImageEnv("POSTGRES_16")
	if img == "" || len(env) == 0 || port != 5432 {
		t.Fatalf("postgres image env=%q %#v %d", img, env, port)
	}
	if img2, _, _ := nestedImageEnv("SQLSERVER_2019"); img2 != "" {
		t.Fatalf("unsupported version should be empty, got %q", img2)
	}
	if normalizeDatabaseVersion("MYSQL") != "MYSQL_8_0" {
		t.Fatalf("normalize MYSQL=%q", normalizeDatabaseVersion("MYSQL"))
	}
	if normalizeDatabaseVersion("POSTGRES") != "POSTGRES_16" {
		t.Fatalf("normalize POSTGRES=%q", normalizeDatabaseVersion("POSTGRES"))
	}
	if normalizeDatabaseVersion("MYSQL_8_0") != "MYSQL_8_0" {
		t.Fatal("normalize passthrough")
	}
	if normalizeDatabaseVersion("nope") != "" {
		t.Fatal("normalize unknown")
	}
	if sanitizeContainerSuffix("AbC_12") != "abc-12" {
		t.Fatalf("sanitize=%q", sanitizeContainerSuffix("AbC_12"))
	}
	if sanitizeContainerSuffix("@@@") != "inst" {
		t.Fatalf("sanitize empty fallback=%q", sanitizeContainerSuffix("@@@"))
	}
}
