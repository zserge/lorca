@echo off
go generate
go build -ldflags "-H windowsgui" -o lorca-example.exe
