---
name: ejecutar-script
description: crear un script, darle permiso de ejecución y correrlo para ver su salida
---
Objetivo: crea prueba.sh que imprima listo, hazlo ejecutable y córrelo
fs_write: prueba.sh | #!/bin/bash\necho listo
Resultado: escrito prueba.sh
shell: chmod +x prueba.sh
Resultado: (sin salida)
[exit 0]
shell: ./prueba.sh
Resultado: listo
[exit 0]
DONE
