---
name: borrar-varios-archivos
description: borrar todos los archivos que cumplan un patrón, por ejemplo todos los .log, y listar lo que queda
---
Objetivo: borra todos los .log y lista los archivos
shell: rm -f *.log
Resultado: (sin salida)
[exit 0]
fs_list: .
Resultado: a.txt
DONE
