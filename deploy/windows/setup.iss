#ifndef AppVersion
  #error AppVersion is required
#endif
#ifndef PackageDir
  #error PackageDir is required
#endif
#ifndef ReleaseDir
  #error ReleaseDir is required
#endif
#ifndef InstallerAppId
  #define InstallerAppId "{{9AF6AF86-48D6-4738-853B-56A76998A1E7}"
#endif
#ifndef InstallerGroupName
  #define InstallerGroupName "DNS Assistant"
#endif

[Setup]
AppId={#InstallerAppId}
AppName=DNS Assistant
AppVersion={#AppVersion}
AppPublisher=DNS Assistant
AppPublisherURL=https://github.com/leoskey/dns-assistant
DefaultDirName={localappdata}\Programs\DNS Assistant
DefaultGroupName={#InstallerGroupName}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir={#ReleaseDir}
OutputBaseFilename=dns-assistant-{#AppVersion}-windows-amd64-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
SetupIconFile=app.ico
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\dns-assistant.exe
CloseApplications=yes
RestartApplications=no
SetupLogging=yes

[Files]
Source: "{#PackageDir}\dns-assistant.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PackageDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PackageDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PackageDir}\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#PackageDir}\Update.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PackageDir}\update.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "installed.flag"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\DNS Assistant"; Filename: "{app}\dns-assistant.exe"; Parameters: "standalone -data ""{localappdata}\DNS Assistant\data"""; WorkingDir: "{app}"
Name: "{group}\Management"; Filename: "{app}\dns-assistant.exe"; Parameters: "standalone -data ""{localappdata}\DNS Assistant\data"""; WorkingDir: "{app}"
Name: "{group}\Update"; Filename: "{app}\Update.cmd"; WorkingDir: "{app}"
Name: "{group}\Readme"; Filename: "{app}\README.md"

[InstallDelete]
Type: files; Name: "{group}\Management.url"

[Run]
Filename: "{app}\dns-assistant.exe"; Parameters: "standalone -data ""{localappdata}\DNS Assistant\data"""; Description: "Start DNS Assistant"; Flags: nowait postinstall skipifsilent unchecked

[Code]
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  StartupCommand: String;
begin
  if CurUninstallStep = usUninstall then
    if RegQueryStringValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'DNS Assistant', StartupCommand) then
      if CompareText(StartupCommand, ExpandConstant('"{app}\dns-assistant.exe" autostart')) = 0 then
        RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'DNS Assistant');
end;
