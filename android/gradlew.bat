@rem
@rem Gradle startup script for Windows
@rem

@if "%DEBUG%"=="" @echo off

set DEFAULT_JVM_OPTS="-Xmx64m" "-Xms64m"

set DIRNAME=%~dp0
if "%DIRNAME%"=="" set DIRNAME=.
set APP_BASE_NAME=%~n0

@rem Find java.exe
if defined JAVA_HOME goto findJavaFromJavaHome

set JAVA_EXE=java.exe
%JAVA_EXE% -version >NUL 2>&1
if %ERRORLEVEL% equ 0 goto execute

echo. 2>&1
echo ERROR: JAVA_HOME is not set and no 'java' command could be found in your PATH. 2>&1
echo. 2>&1
goto fail

:findJavaFromJavaHome
set JAVA_HOME=%JAVA_HOME:"=%
set JAVA_EXE=%JAVA_HOME%/bin/java.exe

if exist "%JAVA_EXE%" goto execute

echo. 2>&1
echo ERROR: JAVA_HOME is set to an invalid directory: %JAVA_HOME% 2>&1
echo. 2>&1
goto fail

:execute
@rem Setup the command line

set CLASSPATH=%DIRNAME%\gradle\wrapper\gradle-wrapper.jar
if exist "%CLASSPATH%" goto runGradle
echo Downloading Gradle wrapper...
powershell -NoProfile -ExecutionPolicy Bypass -Command "Invoke-WebRequest -UseBasicParsing -Uri 'https://raw.githubusercontent.com/gradle/gradle/v8.9.0/gradle/wrapper/gradle-wrapper.jar' -OutFile '%CLASSPATH%'"
if not exist "%CLASSPATH%" (
    echo ERROR: gradle-wrapper.jar is missing and could not be downloaded. 2>&1
    goto fail
)

:runGradle

@rem Execute Gradle
"%JAVA_EXE%" %DEFAULT_JVM_OPTS% %JAVA_OPTS% %GRADLE_OPTS% "-Dorg.gradle.appname=%APP_BASE_NAME%" -classpath "%CLASSPATH%" org.gradle.wrapper.GradleWrapperMain %*

:end
@rem End local scope for the variables with windows extensions enabled
if %ERRORLEVEL% equ 0 goto mainEnd

:fail
rem Set variable GRADLE_EXIT_CONSOLE if you need the _script_ return code instead of
rem the _cmd.exe /c_ return code!
set EXIT_CODE=%ERRORLEVEL%
if %EXIT_CODE% equ 0 set EXIT_CODE=1
if not ""=="%GRADLE_EXIT_CONSOLE%" exit %EXIT_CODE%
exit /b %EXIT_CODE%

:mainEnd
exit /b 0
