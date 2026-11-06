package schemas

import _ "embed"

//go:embed check-request.v1.json
var CheckRequestV1 string

//go:embed report.v1.json
var ReportV1 string
