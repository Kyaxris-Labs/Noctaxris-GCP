param(
    [string]$OutDir = "./lab-ca"
)
$ErrorActionPreference = "Stop"
go run ./scripts/generatelabca $OutDir
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
