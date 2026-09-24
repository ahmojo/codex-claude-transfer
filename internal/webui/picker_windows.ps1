$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$kind = $env:CCT_PICK_KIND
$initial = $env:CCT_PICK_INITIAL
$default = $env:CCT_PICK_DEFAULT
$folder = ''
if ($initial -and [IO.Directory]::Exists($initial)) {
    $folder = $initial
} elseif ($initial) {
    try { $folder = [IO.Path]::GetDirectoryName($initial) } catch {}
}
if (-not $folder -or -not [IO.Directory]::Exists($folder)) {
    if ($default -and [IO.Directory]::Exists($default)) {
        $folder = $default
    } else {
        $folder = [Environment]::GetFolderPath('MyDocuments')
    }
}
$owner = New-Object System.Windows.Forms.Form
$owner.TopMost = $true
$owner.ShowInTaskbar = $false
$owner.StartPosition = 'CenterScreen'
$owner.Size = [System.Drawing.Size]::new(1, 1)
$owner.Opacity = 0
try {
    $owner.Show()
    if ($kind -eq 'folder') {
        $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
        $dialog.Description = 'Choose a folder'
        $dialog.ShowNewFolderButton = $true
        if ($folder) { $dialog.SelectedPath = $folder }
    } elseif ($kind -eq 'save-bundle') {
        $dialog = New-Object System.Windows.Forms.SaveFileDialog
        $dialog.Title = 'Save chat bundle'
        $dialog.Filter = 'cct bundles (*.codexbundle)|*.codexbundle|All files (*.*)|*.*'
        $dialog.DefaultExt = 'codexbundle'
        $dialog.AddExtension = $true
        if ($folder) { $dialog.InitialDirectory = $folder }
        if ($initial -and -not [IO.Directory]::Exists($initial)) {
            $dialog.FileName = [IO.Path]::GetFileName($initial)
        }
    } else {
        $dialog = New-Object System.Windows.Forms.OpenFileDialog
        $dialog.Title = if ($kind -eq 'open-bundle') { 'Choose a chat bundle' } else { 'Choose a file' }
        if ($kind -eq 'open-bundle') {
            $dialog.Filter = 'cct bundles (*.codexbundle;*.age)|*.codexbundle;*.age|All files (*.*)|*.*'
        } else {
            $dialog.Filter = 'All files (*.*)|*.*'
        }
        if ($folder) { $dialog.InitialDirectory = $folder }
        if ($initial -and [IO.File]::Exists($initial)) { $dialog.FileName = $initial }
    }
    try {
        $result = $dialog.ShowDialog($owner)
        if ($result -ne [System.Windows.Forms.DialogResult]::OK) { exit 2 }
        if ($kind -eq 'folder') { [Console]::WriteLine($dialog.SelectedPath) }
        else { [Console]::WriteLine($dialog.FileName) }
    } finally {
        $dialog.Dispose()
    }
} finally {
    $owner.Close()
    $owner.Dispose()
}
