#ifndef AppVersion
  #error AppVersion is required
#endif
#ifndef PackageDir
  #error PackageDir is required
#endif
#ifndef ReleaseDir
  #error ReleaseDir is required
#endif

[Setup]
AppId={{9AF6AF86-48D6-4738-853B-56A76998A1E7}
AppName=DNS Assistant
AppVersion={#AppVersion}
AppPublisher=DNS Assistant
AppPublisherURL=https://github.com/leoskey/dns-assistant
DefaultDirName={localappdata}\Programs\DNS Assistant
DefaultGroupName=DNS Assistant
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir={#ReleaseDir}
OutputBaseFilename=dns-assistant-{#AppVersion}-windows-amd64-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
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

[Icons]
Name: "{group}\DNS Assistant"; Filename: "{app}\dns-assistant.exe"; Parameters: "standalone -data ""{localappdata}\DNS Assistant\data"""; WorkingDir: "{app}"
Name: "{group}\Management"; Filename: "http://127.0.0.1:8080"
Name: "{group}\Readme"; Filename: "{app}\README.md"

[Run]
Filename: "{app}\dns-assistant.exe"; Parameters: "standalone -data ""{localappdata}\DNS Assistant\data"""; Description: "Start DNS Assistant"; Flags: nowait postinstall skipifsilent unchecked
