```bash
./htmldir [-all] [-exit] basedirectory
```
Basedirectory must be provided as an absolute path for the links generated to be correct.  
If -all is provided, all directory listings will be generated/regenerated before settling in to watching.  
-exit will be ignored if -all is not provided. If both -all and -exit are provided, the program will just regenerate all listings and then exit, without doing any watching.  
Note that -arguments must be provided at the beginning and basedirectory as the last argument.
