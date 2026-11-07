---
name: guardar-dato-del-sistema
description: obtener un dato del equipo como la hora, la fecha o el usuario actual y guardarlo en un archivo
---
Objetivo: dime qué hora es y guárdala en hora.txt
shell: date +%H:%M
Resultado: 14:30
[exit 0]
fs_write: hora.txt | 14:30
Resultado: escrito hora.txt
DONE
