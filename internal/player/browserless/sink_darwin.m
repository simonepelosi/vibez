//go:build darwin

#import "sink_darwin.h"
#import <Foundation/Foundation.h>
#import <AVFoundation/AVFoundation.h>
#include <stdlib.h>

@interface VibezPlayerObserver : NSObject
@property (nonatomic, assign) void* userData;
@property (nonatomic, assign) vibez_eos_cb_t eosCb;
@property (nonatomic, assign) vibez_err_cb_t errCb;
@property (nonatomic, weak) AVPlayer* player;
@property (nonatomic, strong) id endObserver;
@property (nonatomic, strong) id errObserver;
@end

@implementation VibezPlayerObserver

- (void)handleEOS:(NSNotification*)note {
    if (self.eosCb) {
        self.eosCb(self.userData);
    }
}

- (void)handleError:(NSNotification*)note {
    if (self.errCb) {
        NSString* desc = @"Playback failed";
        if (self.player && self.player.currentItem && self.player.currentItem.error) {
            desc = [self.player.currentItem.error localizedDescription];
        }
        self.errCb(self.userData, [desc UTF8String]);
    }
}

- (void)cleanupObservers {
    if (self.endObserver) {
        [[NSNotificationCenter defaultCenter] removeObserver:self.endObserver];
        self.endObserver = nil;
    }
    if (self.errObserver) {
        [[NSNotificationCenter defaultCenter] removeObserver:self.errObserver];
        self.errObserver = nil;
    }
}

- (void)dealloc {
    [self cleanupObservers];
}

@end

struct VibezPlayerHandle {
    AVPlayer* player;
    VibezPlayerObserver* observer;
};

vibez_avplayer_t vibez_avplayer_create(void* user_data, vibez_eos_cb_t on_eos, vibez_err_cb_t on_err) {
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)calloc(1, sizeof(struct VibezPlayerHandle));
        if (!h) return NULL;

        h->player = [[AVPlayer alloc] init];
        h->observer = [[VibezPlayerObserver alloc] init];
        h->observer.userData = user_data;
        h->observer.eosCb = on_eos;
        h->observer.errCb = on_err;
        h->observer.player = h->player;

        return (vibez_avplayer_t)h;
    }
}

void vibez_avplayer_play_uri(vibez_avplayer_t handle, const char* uri) {
    if (!handle || !uri) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        [h->observer cleanupObservers];

        NSString* str = [NSString stringWithUTF8String:uri];
        NSURL* url = [NSURL URLWithString:str];
        AVPlayerItem* item = [AVPlayerItem playerItemWithURL:url];

        __weak VibezPlayerObserver* weakObs = h->observer;
        h->observer.endObserver = [[NSNotificationCenter defaultCenter]
            addObserverForName:AVPlayerItemDidPlayToEndTimeNotification
            object:item
            queue:nil
            usingBlock:^(NSNotification *note) {
                [weakObs handleEOS:note];
            }];

        h->observer.errObserver = [[NSNotificationCenter defaultCenter]
            addObserverForName:AVPlayerItemFailedToPlayToEndTimeNotification
            object:item
            queue:nil
            usingBlock:^(NSNotification *note) {
                [weakObs handleError:note];
            }];

        [h->player replaceCurrentItemWithPlayerItem:item];
        [h->player play];
    }
}

void vibez_avplayer_play(vibez_avplayer_t handle) {
    if (!handle) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        [h->player play];
    }
}

void vibez_avplayer_pause(vibez_avplayer_t handle) {
    if (!handle) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        [h->player pause];
    }
}

void vibez_avplayer_stop(vibez_avplayer_t handle) {
    if (!handle) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        [h->player pause];
        [h->observer cleanupObservers];
        [h->player replaceCurrentItemWithPlayerItem:nil];
    }
}

void vibez_avplayer_seek(vibez_avplayer_t handle, double seconds) {
    if (!handle) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        CMTime target = CMTimeMakeWithSeconds(seconds, NSEC_PER_SEC);
        [h->player seekToTime:target toleranceBefore:kCMTimeZero toleranceAfter:kCMTimeZero];
    }
}

void vibez_avplayer_set_volume(vibez_avplayer_t handle, float volume) {
    if (!handle) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        [h->player setVolume:volume];
    }
}

double vibez_avplayer_position(vibez_avplayer_t handle) {
    if (!handle) return 0.0;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        CMTime t = [h->player currentTime];
        if (CMTIME_IS_INVALID(t) || CMTIME_IS_INDEFINITE(t)) {
            return 0.0;
        }
        Float64 s = CMTimeGetSeconds(t);
        return (s >= 0.0) ? s : 0.0;
    }
}

void vibez_avplayer_destroy(vibez_avplayer_t handle) {
    if (!handle) return;
    @autoreleasepool {
        struct VibezPlayerHandle* h = (struct VibezPlayerHandle*)handle;
        [h->player pause];
        [h->observer cleanupObservers];
        [h->player replaceCurrentItemWithPlayerItem:nil];
        h->observer = nil;
        h->player = nil;
        free(h);
    }
}
