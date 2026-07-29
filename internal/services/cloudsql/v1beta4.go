package cloudsql

import "net/http"

// mountV1Beta4 registers the same Admin handlers under /sql/v1beta4 for
// Terraform google_sql_database_instance (provider sql_custom_endpoint).
func (s *Service) mountV1Beta4(mux *http.ServeMux, principalFrom principalFunc) {
	s.mountAPI(mux, principalFrom, "/sql/v1beta4")
}
