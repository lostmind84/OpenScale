@echo off
rem ===========================================================================
rem  start.bat — lance un poste OpenScale À LA MAIN, sans service.
rem
rem  À quoi ça sert : essayer le poste avant de l'installer, ou le faire tourner
rem  cinq minutes sur un PC de bureau pour montrer l'écran client. Le service et
rem  la tâche du kiosque font la même chose, tout seuls, à chaque démarrage — et
rem  c'est install.ps1 qui les pose.
rem
rem  Deux fenêtres s'ouvrent : le poste, et le navigateur. Fermer la fenêtre du
rem  poste arrête tout.
rem
rem  Ce fichier ne demande aucun droit administrateur : c'est ce qui le rend
rem  utile en démonstration.
rem ===========================================================================
setlocal

set "OPENSCALE=%~dp0openscale.exe"
if not exist "%OPENSCALE%" (
  echo openscale.exe est introuvable a cote de start.bat.
  echo Decompressez l'archive complete, puis relancez ce fichier.
  pause
  exit /b 1
)

rem Sans --config ni --data, le poste lit C:\ProgramData\OpenScale, exactement
rem comme le service : une demonstration qui ecrirait ailleurs ne montrerait pas
rem le poste installe.
echo Demarrage du poste...
start "OpenScale - poste" "%OPENSCALE%" serve
timeout /t 3 /nobreak >nul

echo Ouverture de l'ecran client...
start "OpenScale - ecran client" "%OPENSCALE%" kiosk

echo.
echo Le poste tourne. Fermez la fenetre "OpenScale - poste" pour l'arreter.
echo Ecran de depannage : bouton Reglages, en bas a droite, puis Depannage.
