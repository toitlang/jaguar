set VERSION=%1
set BIN_PATH=%2
set BUILD_DIRECTORY=%3

set INSTALLER_NAME="%cd%\tools\windows_installer\jag_installer.exe"

set INNO=ISCC.exe

%INNO% /Qp /dMyAppVersion=%VERSION% /dMyAppExeName="%BIN_PATH%" "%cd%/tools/windows_installer/installer.iss"
move %INSTALLER_NAME% %BUILD_DIRECTORY%
