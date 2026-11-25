# Install the social-mcp binary that matches this Windows machine's CPU.
#
#   irm https://github.com/nhatnam1507/social-mcp/releases/latest/download/install.ps1 | iex
#
# Environment:
#   $env:SOCIAL_MCP_VERSION      release tag to install, for example v0.1.0 (default: latest release)
#   $env:SOCIAL_MCP_INSTALL_DIR  folder to install into (default: %LOCALAPPDATA%\Programs\social-mcp)
#
# Everything runs inside Install-SocialMcp, called on the last line, so a partially downloaded script never
# runs. The script never calls exit, which would close the shell that ran `iex`.

function Install-SocialMcp {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # the progress bar makes Invoke-WebRequest very slow on 5.1

    $repo = 'nhatnam1507/social-mcp'
    $version = if ($env:SOCIAL_MCP_VERSION) { $env:SOCIAL_MCP_VERSION } else { 'latest' }
    $dir = if ($env:SOCIAL_MCP_INSTALL_DIR) { $env:SOCIAL_MCP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\social-mcp' }
    $dir = $dir.TrimEnd('\')

    # The machine-wide value names the native CPU even when this shell runs under emulation.
    $cpu = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment' -ErrorAction SilentlyContinue).PROCESSOR_ARCHITECTURE
    if (-not $cpu) { $cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE } }
    switch ($cpu) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "social-mcp install: unsupported CPU: $cpu (supported: AMD64, ARM64)" }
    }

    if ($version -eq 'latest') {
        $base = "https://github.com/$repo/releases/latest/download"
    } else {
        $base = "https://github.com/$repo/releases/download/$version"
    }
    $asset = "social-mcp-windows-$arch.exe"

    # Windows PowerShell 5.1 may default to TLS 1.0, which GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('social-mcp-' + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "downloading $asset ($version)"
        foreach ($name in @($asset, 'SHA256SUMS')) {
            $url = "$base/$name"
            try {
                Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmp $name) -UseBasicParsing
            } catch {
                throw "social-mcp install: download failed: $url ($($_.Exception.Message))"
            }
        }

        $line = Get-Content (Join-Path $tmp 'SHA256SUMS') |
            Where-Object { ($_ -split '\s+')[1] -in @($asset, "*$asset") } |
            Select-Object -First 1
        if (-not $line) { throw "social-mcp install: SHA256SUMS has no entry for $asset" }
        $expected = ($line -split '\s+')[0]
        $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $asset)).Hash
        if ($expected -ne $actual) { # -ne ignores case
            throw "social-mcp install: checksum mismatch for $asset (expected $expected, got $actual); nothing was installed"
        }

        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        $target = Join-Path $dir 'social-mcp.exe'
        $upgrade = Test-Path $target
        $old = $null
        if ($upgrade) {
            # A running exe (the daemon) cannot be overwritten, but it can be renamed.
            $old = "$target.old"
            Remove-Item $old -Force -ErrorAction SilentlyContinue
            if (Test-Path $old) { $old = "$target.$(Get-Random).old" }
            Move-Item $target $old
        }
        Move-Item (Join-Path $tmp $asset) $target
        if ($old) { Remove-Item $old -Force -ErrorAction SilentlyContinue }

        # Edit the raw registry value so %VARIABLE% entries in the user PATH stay unexpanded.
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $userPath = $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
            $parts = @($userPath -split ';' | Where-Object { $_ } | ForEach-Object { $_.TrimEnd('\') })
            if ($parts -notcontains $dir) {
                $kind = if ($key.GetValueNames() -contains 'Path') { $key.GetValueKind('Path') } else { 'ExpandString' }
                $key.SetValue('Path', ((@($userPath.TrimEnd(';')) + $dir | Where-Object { $_ }) -join ';'), $kind)
                # Setting any user variable through .NET broadcasts the change to new terminals.
                [Environment]::SetEnvironmentVariable('SOCIAL_MCP_INSTALLER', '1', 'User')
                [Environment]::SetEnvironmentVariable('SOCIAL_MCP_INSTALLER', $null, 'User')
                Write-Host "added $dir to your user PATH; open a new terminal to use social-mcp there"
            }
        } finally {
            $key.Close()
        }
        if (($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') }) -notcontains $dir) { $env:Path = "$env:Path;$dir" }

        $installed = & $target version
        Write-Host "installed social-mcp $installed to $target"
        if ($upgrade) {
            Write-Host 'A running daemon keeps the old version until you restart it: social-mcp daemon stop; social-mcp daemon start'
        }
    } finally {
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-SocialMcp
