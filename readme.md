```bash
./htmldir [-all] [-exit] basedirectory
```
Basedirectory must be provided as an absolute path for the links generated to be correct. Use $PWD instead of '.'.  
If -all is provided, all directory listings will be generated/regenerated before settling in to watching.  
-exit will be ignored if -all is not provided. If both -all and -exit are provided, the program will just regenerate all listings and then exit, without doing any watching.  
Note that -arguments must be provided at the beginning and basedirectory as the last argument.  
Intended for Linux.

If you see `no space left on device`, it is probably because you have reached the maximum number of inotify watches. Use `cat /proc/sys/fs/inotify/max_user_watches` to see the current number and `sudo sysctl fs.inotify.max_user_watches=number` to temporarily change it. Also see https://unix.stackexchange.com/questions/13751/kernel-inotify-watch-limit-reached.
