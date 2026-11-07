---
name: sumar-columna
description: sumar, promediar o calcular el máximo de los números de un archivo o de una columna
---
Objetivo: suma los números de numeros.txt
shell: awk '{s+=$1} END {print s}' numeros.txt
Resultado: 150
[exit 0]
DONE
