@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-all.ps1" %*
set "BUILD_EXIT_CODE=%ERRORLEVEL%"
echo.
if not "%BUILD_EXIT_CODE%"=="0" (
    echo PhoneVRStream build failed with exit code %BUILD_EXIT_CODE%.
) else (
    echo PhoneVRStream build completed successfully.
)
exit /b %BUILD_EXIT_CODE%
