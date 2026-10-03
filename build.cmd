@echo off
setlocal
cd /d "%~dp0"
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
if not exist dist mkdir dist
go test ./...
if errorlevel 1 exit /b 1
go build -buildvcs=false -trimpath -ldflags="-s -w -H windowsgui" -o dist\Finalizer_Librarian_v1.3.exe .
if errorlevel 1 exit /b 1
echo Build completed: dist\Finalizer_Librarian_v1.3.exe
