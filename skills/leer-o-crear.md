---
name: leer-o-crear
description: leer un archivo que quizá no existe y, si falta, crearlo con un contenido por defecto
---
Objetivo: lee datos.txt y si no existe créalo con la palabra hola
fs_read: datos.txt
Error: ENOENT: no existe datos.txt
fs_write: datos.txt | hola
Resultado: escrito datos.txt
DONE
