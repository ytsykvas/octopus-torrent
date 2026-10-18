#ifndef OCTOPUS_TRASH_DARWIN_H
#define OCTOPUS_TRASH_DARWIN_H

// trash_file moves the file at path to the macOS Trash with NSFileManager. On success it returns the
// item's new location there (the caller frees it) and leaves *err NULL. On failure it returns NULL
// and sets *err to the error description (the caller frees it). It never deletes irreversibly.
char *trash_file(const char *path, char **err);

#endif
