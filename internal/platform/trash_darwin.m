#import <Foundation/Foundation.h>
#include <stdlib.h>
#include "trash_darwin.h"

char *trash_file(const char *path, char **err) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        NSURL *resulting = nil;
        NSError *nserr = nil;
        if (![[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:&resulting error:&nserr]) {
            *err = strdup([[nserr localizedDescription] UTF8String]);
            return NULL;
        }
        return strdup([[resulting path] UTF8String]);
    }
}
