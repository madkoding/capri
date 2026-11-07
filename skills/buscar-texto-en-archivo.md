---
name: buscar-texto-en-archivo
description: contar cuántas veces aparece una palabra o texto dentro de un archivo, buscar apariciones de una cadena
---
Objetivo: cuántas veces aparece la palabra gato en cuento.txt
shell: grep -o gato cuento.txt | wc -l
Resultado: 4
[exit 0]
DONE
