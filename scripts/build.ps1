param(
    # Output directory for all cross-compiled binaries.
    [string]$OutDir = "dist"
)

# Fail fast so CI and local runs stop on first broken build target.
New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
# Build each module across all OS/arch targets used by this project.
$bins = @('agent','controller','relay')
$oses = @('windows','linux','darwin')
$archs = @('amd64','arm64')

# Triple nested loop emits one binary per module/OS/arch combination.
foreach ($os in $oses) {
    foreach ($arch in $archs) {
        foreach ($b in $bins) {
            $out = "$OutDir\$b-$os-$arch"
            if ($os -eq 'windows') { $out += '.exe' }
            Write-Host "Building $b for $os/$arch -> $out"
            $env:GOOS = $os
            $env:GOARCH = $arch
            go build -o $out "./$b"
        }
    }
}

Write-Host "Builds written to $OutDir"
