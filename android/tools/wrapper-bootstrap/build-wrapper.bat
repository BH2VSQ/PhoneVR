@echo off
setlocal
set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..\..") do set "ANDROID_ROOT=%%~fI"
set "WRAPPER_DIR=%ANDROID_ROOT%\gradle\wrapper"
set "WRAPPER_JAR=%WRAPPER_DIR%\gradle-wrapper.jar"
set "SOURCE=%SCRIPT_DIR%src\org\gradle\wrapper\GradleWrapperMain.java"

if exist "%WRAPPER_JAR%" exit /b 0

set "JAVAC_EXE="
set "JAR_EXE="
if defined JAVA_HOME if exist "%JAVA_HOME%\bin\javac.exe" set "JAVAC_EXE=%JAVA_HOME%\bin\javac.exe"
if defined JAVA_HOME if exist "%JAVA_HOME%\bin\jar.exe" set "JAR_EXE=%JAVA_HOME%\bin\jar.exe"
if not defined JAVAC_EXE if exist "%ProgramFiles%\Android\Android Studio\jbr\bin\javac.exe" set "JAVAC_EXE=%ProgramFiles%\Android\Android Studio\jbr\bin\javac.exe"
if not defined JAR_EXE if exist "%ProgramFiles%\Android\Android Studio\jbr\bin\jar.exe" set "JAR_EXE=%ProgramFiles%\Android\Android Studio\jbr\bin\jar.exe"
if not defined JAVAC_EXE if exist "%LOCALAPPDATA%\Programs\Android Studio\jbr\bin\javac.exe" set "JAVAC_EXE=%LOCALAPPDATA%\Programs\Android Studio\jbr\bin\javac.exe"
if not defined JAR_EXE if exist "%LOCALAPPDATA%\Programs\Android Studio\jbr\bin\jar.exe" set "JAR_EXE=%LOCALAPPDATA%\Programs\Android Studio\jbr\bin\jar.exe"
if not defined JAVAC_EXE for /f "delims=" %%I in ('where javac.exe 2^>nul') do if not defined JAVAC_EXE set "JAVAC_EXE=%%I"
if not defined JAR_EXE for /f "delims=" %%I in ('where jar.exe 2^>nul') do if not defined JAR_EXE set "JAR_EXE=%%I"

if not defined JAVAC_EXE (
  echo Gradle bootstrap JAR is missing and javac.exe was not found. 1>&2
  echo Set JAVA_HOME to a JDK 8+ installation. 1>&2
  exit /b 1
)
if not defined JAR_EXE (
  echo Gradle bootstrap JAR is missing and jar.exe was not found. 1>&2
  echo Set JAVA_HOME to a JDK 8+ installation. 1>&2
  exit /b 1
)
if not exist "%SOURCE%" (
  echo Gradle bootstrap source is missing: %SOURCE% 1>&2
  exit /b 1
)

set "BUILD_DIR=%TEMP%\phonevr-gradle-wrapper-%RANDOM%-%RANDOM%"
mkdir "%BUILD_DIR%\classes" 2>nul
if errorlevel 1 exit /b 1
(
  echo Manifest-Version: 1.0
  echo Main-Class: org.gradle.wrapper.GradleWrapperMain
  echo.
) > "%BUILD_DIR%\MANIFEST.MF"
"%JAVAC_EXE%" -source 8 -target 8 -Xlint:-options -encoding UTF-8 -d "%BUILD_DIR%\classes" "%SOURCE%"
if errorlevel 1 goto :failed
"%JAR_EXE%" cfm "%BUILD_DIR%\gradle-wrapper.jar" "%BUILD_DIR%\MANIFEST.MF" -C "%BUILD_DIR%\classes" .
if errorlevel 1 goto :failed
"%JAR_EXE%" tf "%BUILD_DIR%\gradle-wrapper.jar" | findstr /x /c:"org/gradle/wrapper/GradleWrapperMain.class" >nul
if errorlevel 1 goto :failed
if not exist "%WRAPPER_DIR%" mkdir "%WRAPPER_DIR%"
move /y "%BUILD_DIR%\gradle-wrapper.jar" "%WRAPPER_JAR%" >nul
if errorlevel 1 goto :failed
rmdir /s /q "%BUILD_DIR%" 2>nul
echo Generated PhoneVR Gradle bootstrap JAR: %WRAPPER_JAR%
exit /b 0

:failed
set "EXIT_CODE=%ERRORLEVEL%"
rmdir /s /q "%BUILD_DIR%" 2>nul
if "%EXIT_CODE%"=="0" set "EXIT_CODE=1"
exit /b %EXIT_CODE%
