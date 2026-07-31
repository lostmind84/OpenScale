@echo off
rem ===========================================================================
rem  bootstrap.cmd - installe un poste de pesee OpenScale en une commande.
rem
rem  A taper dans une invite de commandes, elevee ou non :
rem
rem    curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.cmd -o %TEMP%\openscale.cmd ^&^& %TEMP%\openscale.cmd
rem
rem  Ce fichier ne fait rien de plus que rappeler bootstrap.ps1, qui porte toute
rem  l'installation : deux entrees, une seule logique. Le -ExecutionPolicy
rem  Bypass est ce qui evite au benevole la strategie d'execution par defaut de
rem  Windows, qui refuse les scripts venus d'Internet.
rem
rem  Sans accent, et ce n'est pas un oubli : cmd.exe lit ce fichier en CP850, et
rem  toute lettre accentuee sort de travers a l'ecran. start.bat le sait depuis
rem  le premier jour, et un test de deploy/ le verifie lettre par lettre.
rem ===========================================================================
setlocal

set "OPENSCALE_BOOTSTRAP=https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1"

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12; iex ((New-Object Net.WebClient).DownloadString('%OPENSCALE_BOOTSTRAP%'))"
exit /b %ERRORLEVEL%
