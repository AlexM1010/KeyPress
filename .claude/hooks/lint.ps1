# Runs the linters once at the end of a turn, and hands any failure back.
#
# Deliberately not a PostToolUse hook: golangci-lint and ESLint each take
# seconds, and running them per-edit would make a ten-edit turn crawl. Once per
# turn is the right granularity - the work is finished, and there is something
# to check.
#
# Exit 2 is the whole point. On Stop it feeds stderr back to Claude, which then
# fixes the finding and continues, rather than the failure surfacing later in CI
# or not at all. Exit 0 stays silent.
#
# Only the side that actually changed is checked, so a Go-only turn does not pay
# for ESLint. `git diff HEAD --name-only` covers staged and unstaged together;
# untracked files are added separately.

$ErrorActionPreference = 'Continue'

$root = $env:CLAUDE_PROJECT_DIR
if (-not $root -or -not (Test-Path -LiteralPath $root)) { exit 0 }

Set-Location -LiteralPath $root

$changed = @()
try {
	$changed += (& git diff HEAD --name-only 2>$null)
	$changed += (& git ls-files --others --exclude-standard 2>$null)
} catch {
	exit 0
}
$changed = $changed | Where-Object { $_ }
if (-not $changed) { exit 0 }

$goTouched = @($changed | Where-Object { $_ -match '\.go$' -or $_ -match '^go\.(mod|sum)$' }).Count -gt 0
$feTouched = @($changed | Where-Object { $_ -match '^frontend/.*\.(ts|js|svelte|css|scss|json)$' }).Count -gt 0

$problems = @()

if ($goTouched) {
	# golangci-lint loads package main, which embeds frontend/build - so on a
	# tree where the frontend has never been built it fails for a reason that
	# has nothing to do with the edit. Skip rather than report that confusion.
	if (Test-Path -LiteralPath (Join-Path $root 'frontend\build')) {
		$golangci = Join-Path $env:USERPROFILE 'go\bin\golangci-lint.exe'
		if (Test-Path -LiteralPath $golangci) {
			$out = & $golangci run ./... 2>&1
			$text = $out -join "`n"
			# golangci-lint takes a lock and refuses to run twice at once. A
			# second session or a subagent linting concurrently is not a finding
			# about this code, so it must not be reported as one.
			if ($LASTEXITCODE -ne 0 -and $text -notmatch 'parallel golangci-lint is running') {
				$problems += "golangci-lint:`n$text"
			}
		}
	}
}

if ($feTouched) {
	$frontend = Join-Path $root 'frontend'
	if (Test-Path -LiteralPath (Join-Path $frontend 'node_modules\.bin\eslint.cmd')) {
		Push-Location $frontend
		try {
			$out = & npx --no-install eslint . 2>&1
			if ($LASTEXITCODE -ne 0) { $problems += "eslint:`n$($out -join "`n")" }

			$out = & npx --no-install prettier --check . 2>&1
			if ($LASTEXITCODE -ne 0) { $problems += "prettier:`n$($out -join "`n")" }
		} finally {
			Pop-Location
		}
	}
}

if ($problems.Count -gt 0) {
	[Console]::Error.WriteLine(($problems -join "`n`n"))
	exit 2
}

exit 0
