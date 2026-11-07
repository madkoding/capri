---
name: contar-por-extension
description: contar cuántos archivos hay de una extensión o tipo, como .py, .txt o .log, en la carpeta
---
Objetivo: cuántos archivos .py hay
shell: ls *.py 2>/dev/null | wc -l
Resultado: 3
[exit 0]
DONE
