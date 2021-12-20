set VERSION=%1
set BIN_PATH=%2
set BUILD_DIRECTORY=%3

set INSTALLER_NAME="%cd%\tools\windows_installer\jag_installer.exe"

set inno="C:\Program Files (x86)\Inno Setup 6\ISCC.exe"

%inno% /Qp /dMyAppVersion=%TOIT_VERSION% /dMyAppExeName="%BIN_PATH%" "%cd%/tools/windows_installer/installer.iss"
move %INSTALLER_NAME% %BUILD_DIRECTORY%
