@echo off
setlocal
set "APP_HOME=%~dp0"
set "JAVA_EXE=java.exe"
set "WRAPPER_JAR=%APP_HOME%gradle\wrapper\gradle-wrapper.jar"

if defined JAVA_HOME if exist "%JAVA_HOME%\bin\java.exe" set "JAVA_EXE=%JAVA_HOME%\bin\java.exe"

if not defined JAVA_HOME (
  if exist "%ProgramFiles%\Android\Android Studio\jbr\bin\java.exe" set "JAVA_EXE=%ProgramFiles%\Android\Android Studio\jbr\bin\java.exe"
  if exist "%LOCALAPPDATA%\Programs\Android Studio\jbr\bin\java.exe" set "JAVA_EXE=%LOCALAPPDATA%\Programs\Android Studio\jbr\bin\java.exe"
)

if not exist "%WRAPPER_JAR%" (
  call "%APP_HOME%tools\wrapper-bootstrap\build-wrapper.bat"
  if errorlevel 1 (
    endlocal
    exit /b 1
  )
)

"%JAVA_EXE%" -Dorg.gradle.appname=gradlew -classpath "%WRAPPER_JAR%" org.gradle.wrapper.GradleWrapperMain %*
set "EXIT_CODE=%ERRORLEVEL%"
endlocal & exit /b %EXIT_CODE%
