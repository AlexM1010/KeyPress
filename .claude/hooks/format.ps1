# Formats a single file after Claude edits it.
#
# Runs on every Edit/Write, so it has to be fast and it has to be quiet: only
# the one file, only formatters, never a linter or a type check. Those live in
# lint.ps1, which runs once at the end of a turn instead of once per edit.
#
# Always exits 0. A formatter that cannot run is not a reason to interrupt the
# work - the Stop hook and CI both still gate the real checks.

$ErrorActionPreference = 'Stop'

try {
	$raw = [Console]::In.ReadToEnd()
	if (-not $raw) { exit 0 }
	$payload = $raw | ConvertFrom-Json
} catch {
	exit 0
}

$path = $payload.tool_input.file_path
if (-not $path -or -not (Test-Path -LiteralPath $path)) { exit 0 }

$root = $env:CLAUDE_PROJECT_DIR
if (-not $root) { exit 0 }

# Generated Wails bindings are rewritten wholesale by `wails3 generate bindings`
# (-clean=true), so formatting them is churn that the next build discards.
if ($path -match 'frontend[\\/]src[\\/]lib[\\/]bindings[\\/]') { exit 0 }
if ($path -match 'node_modules|[\\/]\.svelte-kit[\\/]|[\\/]build[\\/]|[\\/]dist[\\/]') { exit 0 }

try {
	switch -Regex ($path) {
		'\.go$' {
			& gofmt -w -- $path 2>&1 | Out-Null
		}
		'\.(ts|js|svelte|css|scss|json|html|md)$' {
			$frontend = Join-Path $root 'frontend'
			if (Test-Path -LiteralPath (Join-Path $frontend 'node_modules\.bin\prettier.cmd')) {
				Push-Location $frontend
				try {
					& npx --no-install prettier --write --ignore-unknown -- $path 2>&1 | Out-Null
				} finally {
					Pop-Location
				}
			}
		}
	}
} catch {
	# Deliberately swallowed. See the header.
}

exit 0
