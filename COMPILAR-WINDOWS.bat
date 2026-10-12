@echo off
setlocal
cd /d "%~dp0"
where go >nul 2>nul || (echo Go nao encontrado. Instale https://go.dev/dl/ & pause & exit /b 1)
echo Testando codigo...
go test ./... || (pause & exit /b 1)
echo Compilando app nativo Windows x64...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags="-H=windowsgui -s -w" -o Astronyx-Steam-Manager.exe . || (pause & exit /b 1)
echo Pronto: Astronyx-Steam-Manager.exe
pause
