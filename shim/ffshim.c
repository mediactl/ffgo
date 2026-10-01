/*
 * ffshim.c - FFmpeg shim library for ffgo
 *
 * Provides wrappers for FFmpeg functionality that purego cannot handle directly.
 */

#include "ffshim.h"

#include <libavutil/log.h>
#include <libavutil/rational.h>
#include <libavutil/error.h>
#include <libavutil/avutil.h>
#include <libavutil/channel_layout.h>
#include <libavutil/version.h>
#include <libavutil/mem.h>
#include <libavutil/frame.h>
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavformat/avio.h>

#include <stdio.h>
#include <stdarg.h>
#include <string.h>

/* ============================================================================
 * LOGGING SUBSYSTEM
 * ============================================================================ */

/* Global callback pointer - set by Go */
static ffshim_log_callback_t g_log_callback = NULL;

/* Internal callback that FFmpeg calls - formats the message then calls Go */
static void internal_log_callback(void *avcl, int level, const char *fmt, va_list vl) {
    if (g_log_callback == NULL) {
        return;
    }

    char buf[4096];
    int len = vsnprintf(buf, sizeof(buf), fmt, vl);

    /* Remove trailing newline if present (Go will add its own) */
    if (len > 0 && len < (int)sizeof(buf) && buf[len-1] == '\n') {
        buf[len-1] = '\0';
    }

    g_log_callback(avcl, level, buf);
}

/* Called by Go to set up logging */
void ffshim_log_set_callback(ffshim_log_callback_t cb) {
    g_log_callback = cb;
    if (cb != NULL) {
        av_log_set_callback(internal_log_callback);
    } else {
        av_log_set_callback(av_log_default_callback);
    }
}

/* Called by Go to set log level */
void ffshim_log_set_level(int level) {
    av_log_set_level(level);
}

/* Called by Go to log a pre-formatted message */
void ffshim_log(void *avcl, int level, const char *msg) {
    av_log(avcl, level, "%s", msg);
}

/* ============================================================================
 * AVRATIONAL OPERATIONS (for non-Darwin platforms)
 * ============================================================================ */

void ffshim_rational_mul(int a_num, int a_den, int b_num, int b_den, int *out_num, int *out_den) {
    AVRational a = {a_num, a_den};
    AVRational b = {b_num, b_den};
    AVRational result = av_mul_q(a, b);
    *out_num = result.num;
    *out_den = result.den;
}

void ffshim_rational_div(int a_num, int a_den, int b_num, int b_den, int *out_num, int *out_den) {
    AVRational a = {a_num, a_den};
    AVRational b = {b_num, b_den};
    AVRational result = av_div_q(a, b);
    *out_num = result.num;
    *out_den = result.den;
}

void ffshim_rational_add(int a_num, int a_den, int b_num, int b_den, int *out_num, int *out_den) {
    AVRational a = {a_num, a_den};
    AVRational b = {b_num, b_den};
    AVRational result = av_add_q(a, b);
    *out_num = result.num;
    *out_den = result.den;
}

void ffshim_rational_sub(int a_num, int a_den, int b_num, int b_den, int *out_num, int *out_den) {
    AVRational a = {a_num, a_den};
    AVRational b = {b_num, b_den};
    AVRational result = av_sub_q(a, b);
    *out_num = result.num;
    *out_den = result.den;
}

void ffshim_d2q(double d, int max_den, int *out_num, int *out_den) {
    AVRational result = av_d2q(d, max_den);
    *out_num = result.num;
    *out_den = result.den;
}

double ffshim_q2d(int num, int den) {
    AVRational q = {num, den};
    return av_q2d(q);
}

int ffshim_rational_cmp(int a_num, int a_den, int b_num, int b_den) {
    AVRational a = {a_num, a_den};
    AVRational b = {b_num, b_den};
    return av_cmp_q(a, b);
}

/* ============================================================================
 * ERROR HANDLING
 * ============================================================================ */

int ffshim_strerror(int errnum, char *errbuf, size_t errbuf_size) {
    return av_strerror(errnum, errbuf, errbuf_size);
}

/* ============================================================================
 * AVIO HELPERS
 * ============================================================================ */

void ffshim_avio_write_string(void *avio_ctx, const char *str) {
    avio_write((AVIOContext*)avio_ctx, (const unsigned char*)str, strlen(str));
}

/* ============================================================================
 * CHAPTER HELPERS
 * ============================================================================ */

void* ffshim_new_chapter(void *ctx, int64_t id, int tb_num, int tb_den, int64_t start, int64_t end, void *metadata) {
    if (ctx == NULL) {
        return NULL;
    }

    AVFormatContext *fc = (AVFormatContext *)ctx;
    AVChapter *ch = (AVChapter *)av_mallocz(sizeof(AVChapter));
    if (ch == NULL) {
        return NULL;
    }

    ch->id = id;
    ch->time_base = (AVRational){tb_num, tb_den};
    ch->start = start;
    ch->end = end;
    ch->metadata = (AVDictionary *)metadata; // take ownership

    // Grow chapters array
    unsigned int newCount = fc->nb_chapters + 1;
    AVChapter **newArr = (AVChapter **)av_realloc_array(fc->chapters, newCount, sizeof(AVChapter *));
    if (newArr == NULL) {
        // If we took metadata ownership, keep it with the chapter so caller doesn't free it.
        av_free(ch);
        return NULL;
    }
    fc->chapters = newArr;
    fc->chapters[fc->nb_chapters] = ch;
    fc->nb_chapters = newCount;

    return ch;
}

/* ============================================================================
 * VERSION INFO
 * ============================================================================ */

unsigned int ffshim_avutil_version(void) {
    return avutil_version();
}

unsigned int ffshim_avcodec_version(void) {
    return avcodec_version();
}

unsigned int ffshim_avformat_version(void) {
    return avformat_version();
}

/* ============================================================================
 * AVDEVICE HELPERS (OPTIONAL)
 * ============================================================================ */

#ifdef FFSHIM_HAVE_AVDEVICE

#include <libavdevice/avdevice.h>

static int g_avdevice_registered = 0;

int ffshim_avdevice_list_input_sources(
    const char *format_name,
    const char *device_name,
    void *avdict_opts,
    int *out_count,
    char ***out_names,
    char ***out_descs
) {
    if (out_count == NULL || out_names == NULL || out_descs == NULL) {
        return AVERROR(EINVAL);
    }

    *out_count = 0;
    *out_names = NULL;
    *out_descs = NULL;

    if (format_name == NULL || format_name[0] == '\0') {
        return AVERROR(EINVAL);
    }

    if (!g_avdevice_registered) {
        avdevice_register_all();
        g_avdevice_registered = 1;
    }

    const AVInputFormat *fmt = av_find_input_format(format_name);
    if (fmt == NULL) {
        return AVERROR(EINVAL);
    }

    AVDeviceInfoList *list = NULL;
    AVDictionary *dict = (AVDictionary *)avdict_opts;

    int ret = avdevice_list_input_sources(fmt,
                                         (device_name != NULL && device_name[0] != '\0') ? device_name : NULL,
                                         dict,
                                         &list);
    if (ret < 0) {
        if (list != NULL) {
            avdevice_free_list_devices(&list);
        }
        return ret;
    }
    if (list == NULL || list->nb_devices <= 0) {
        if (list != NULL) {
            avdevice_free_list_devices(&list);
        }
        return 0;
    }

    int count = list->nb_devices;

    char **names = (char **)av_mallocz((size_t)count * sizeof(char *));
    char **descs = (char **)av_mallocz((size_t)count * sizeof(char *));
    if (names == NULL || descs == NULL) {
        if (names != NULL) av_free(names);
        if (descs != NULL) av_free(descs);
        avdevice_free_list_devices(&list);
        return AVERROR(ENOMEM);
    }

    for (int i = 0; i < count; i++) {
        AVDeviceInfo *dev = list->devices[i];
        const char *dn = (dev != NULL && dev->device_name != NULL) ? dev->device_name : "";
        const char *dd = (dev != NULL && dev->device_description != NULL) ? dev->device_description : "";
        names[i] = av_strdup(dn);
        descs[i] = av_strdup(dd);
    }

    avdevice_free_list_devices(&list);

    *out_count = count;
    *out_names = names;
    *out_descs = descs;
    return 0;
}

void ffshim_avdevice_free_string_array(char **arr, int count) {
    if (arr == NULL || count <= 0) {
        return;
    }
    for (int i = 0; i < count; i++) {
        if (arr[i] != NULL) {
            av_free(arr[i]);
        }
    }
    av_free(arr);
}

#else

int ffshim_avdevice_list_input_sources(
    const char *format_name,
    const char *device_name,
    void *avdict_opts,
    int *out_count,
    char ***out_names,
    char ***out_descs
) {
    (void)format_name;
    (void)device_name;
    (void)avdict_opts;
    if (out_count) *out_count = 0;
    if (out_names) *out_names = NULL;
    if (out_descs) *out_descs = NULL;
    return AVERROR(ENOSYS);
}

void ffshim_avdevice_free_string_array(char **arr, int count) {
    (void)arr;
    (void)count;
}

#endif

/* ============================================================================
 * AVFRAME OFFSET HELPERS (OPTIONAL)
 * ============================================================================ */

int ffshim_avframe_color_offsets(
    int *out_color_range,
    int *out_colorspace,
    int *out_color_primaries,
    int *out_color_trc
) {
    if (out_color_range == NULL || out_colorspace == NULL || out_color_primaries == NULL || out_color_trc == NULL) {
        return -1;
    }

    *out_color_range = (int)offsetof(AVFrame, color_range);
    *out_colorspace = (int)offsetof(AVFrame, colorspace);
    *out_color_primaries = (int)offsetof(AVFrame, color_primaries);
    *out_color_trc = (int)offsetof(AVFrame, color_trc);
    return 0;
}

/* ============================================================================
 * CODEC FIELD HELPERS (OPTIONAL)
 * ============================================================================ */

int ffshim_codecpar_width(void *par) {
    if (par == NULL) {
        return 0;
    }
    return ((AVCodecParameters*)par)->width;
}

int ffshim_codecpar_height(void *par) {
    if (par == NULL) {
        return 0;
    }
    return ((AVCodecParameters*)par)->height;
}

int ffshim_codecpar_format(void *par) {
    if (par == NULL) {
        return -1;
    }
    return ((AVCodecParameters*)par)->format;
}

int ffshim_codecpar_sample_rate(void *par) {
    if (par == NULL) {
        return 0;
    }
    return ((AVCodecParameters*)par)->sample_rate;
}

int ffshim_codecpar_channels(void *par) {
    if (par == NULL) {
        return 0;
    }
    return ((AVCodecParameters*)par)->ch_layout.nb_channels;
}

int ffshim_codecctx_width(void *ctx) {
    if (ctx == NULL) {
        return 0;
    }
    return ((AVCodecContext*)ctx)->width;
}

void ffshim_codecctx_set_width(void *ctx, int width) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->width = width;
}

int ffshim_codecctx_height(void *ctx) {
    if (ctx == NULL) {
        return 0;
    }
    return ((AVCodecContext*)ctx)->height;
}

void ffshim_codecctx_set_height(void *ctx, int height) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->height = height;
}

int ffshim_codecctx_pix_fmt(void *ctx) {
    if (ctx == NULL) {
        return -1;
    }
    return ((AVCodecContext*)ctx)->pix_fmt;
}

void ffshim_codecctx_set_pix_fmt(void *ctx, int pix_fmt) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->pix_fmt = pix_fmt;
}

int ffshim_codecctx_sample_fmt(void *ctx) {
    if (ctx == NULL) {
        return -1;
    }
    return ((AVCodecContext*)ctx)->sample_fmt;
}

void ffshim_codecctx_set_sample_fmt(void *ctx, int sample_fmt) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->sample_fmt = sample_fmt;
}

void ffshim_codecctx_time_base(void *ctx, int *out_num, int *out_den) {
    if (ctx == NULL || out_num == NULL || out_den == NULL) {
        return;
    }
    *out_num = ((AVCodecContext*)ctx)->time_base.num;
    *out_den = ((AVCodecContext*)ctx)->time_base.den;
}

void ffshim_codecctx_set_time_base(void *ctx, int num, int den) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->time_base = (AVRational){num, den};
}

void ffshim_codecctx_framerate(void *ctx, int *out_num, int *out_den) {
    if (ctx == NULL || out_num == NULL || out_den == NULL) {
        return;
    }
    *out_num = ((AVCodecContext*)ctx)->framerate.num;
    *out_den = ((AVCodecContext*)ctx)->framerate.den;
}

void ffshim_codecctx_set_framerate(void *ctx, int num, int den) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->framerate = (AVRational){num, den};
}

void ffshim_codecctx_set_ch_layout_default(void *ctx, int nb_channels) {
#if LIBAVUTIL_VERSION_MAJOR >= 57
    if (ctx == NULL) {
        return;
    }
    AVCodecContext *c = (AVCodecContext*)ctx;
    av_channel_layout_default(&c->ch_layout, nb_channels);
#else
    (void)ctx;
    (void)nb_channels;
#endif
}

void* ffshim_codecctx_hw_device_ctx(void *ctx) {
    if (ctx == NULL) {
        return NULL;
    }
    return (void*)((AVCodecContext*)ctx)->hw_device_ctx;
}

void ffshim_codecctx_set_hw_device_ctx(void *ctx, void *ref) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->hw_device_ctx = (AVBufferRef*)ref;
}

void* ffshim_codecctx_hw_frames_ctx(void *ctx) {
    if (ctx == NULL) {
        return NULL;
    }
    return (void*)((AVCodecContext*)ctx)->hw_frames_ctx;
}

void ffshim_codecctx_set_hw_frames_ctx(void *ctx, void *ref) {
    if (ctx == NULL) {
        return;
    }
    ((AVCodecContext*)ctx)->hw_frames_ctx = (AVBufferRef*)ref;
}

/* ============================================================================
 * FORMAT FIELD HELPERS (OPTIONAL)
 * ============================================================================ */

int64_t ffshim_formatctx_duration(void *ctx) {
    if (ctx == NULL) {
        return 0;
    }
    return ((AVFormatContext*)ctx)->duration;
}

int64_t ffshim_formatctx_bit_rate(void *ctx) {
    if (ctx == NULL) {
        return 0;
    }
    return ((AVFormatContext*)ctx)->bit_rate;
}

unsigned int ffshim_formatctx_nb_chapters(void *ctx) {
    if (ctx == NULL) {
        return 0;
    }
    return ((AVFormatContext*)ctx)->nb_chapters;
}

void* ffshim_formatctx_chapter(void *ctx, int index) {
    if (ctx == NULL || index < 0) {
        return NULL;
    }
    AVFormatContext *fc = (AVFormatContext*)ctx;
    if (fc->chapters == NULL) {
        return NULL;
    }
    if ((unsigned int)index >= fc->nb_chapters) {
        return NULL;
    }
    return (void*)fc->chapters[index];
}

unsigned int ffshim_formatctx_nb_programs(void *ctx) {
    if (ctx == NULL) {
        return 0;
    }
    return ((AVFormatContext*)ctx)->nb_programs;
}

void* ffshim_formatctx_program(void *ctx, int index) {
    if (ctx == NULL || index < 0) {
        return NULL;
    }
    AVFormatContext *fc = (AVFormatContext*)ctx;
    if (fc->programs == NULL) {
        return NULL;
    }
    if ((unsigned int)index >= fc->nb_programs) {
        return NULL;
    }
    return (void*)fc->programs[index];
}

int64_t ffshim_chapter_id(void *ch) {
    if (ch == NULL) {
        return 0;
    }
    return ((AVChapter*)ch)->id;
}

void ffshim_chapter_time_base(void *ch, int *out_num, int *out_den) {
    if (ch == NULL || out_num == NULL || out_den == NULL) {
        return;
    }
    *out_num = ((AVChapter*)ch)->time_base.num;
    *out_den = ((AVChapter*)ch)->time_base.den;
}

int64_t ffshim_chapter_start(void *ch) {
    if (ch == NULL) {
        return 0;
    }
    return ((AVChapter*)ch)->start;
}

int64_t ffshim_chapter_end(void *ch) {
    if (ch == NULL) {
        return 0;
    }
    return ((AVChapter*)ch)->end;
}

void* ffshim_chapter_metadata(void *ch) {
    if (ch == NULL) {
        return NULL;
    }
    return (void*)((AVChapter*)ch)->metadata;
}

int ffshim_program_id(void *p) {
    if (p == NULL) {
        return 0;
    }
    return ((AVProgram*)p)->id;
}

unsigned int ffshim_program_nb_stream_indexes(void *p) {
    if (p == NULL) {
        return 0;
    }
    return ((AVProgram*)p)->nb_stream_indexes;
}

unsigned int* ffshim_program_stream_index(void *p) {
    if (p == NULL) {
        return NULL;
    }
    return ((AVProgram*)p)->stream_index;
}

void* ffshim_program_metadata(void *p) {
    if (p == NULL) {
        return NULL;
    }
    return (void*)((AVProgram*)p)->metadata;
}

/* ============================================================================
 * STRUCT LAYOUT (offsets as the headers this shim was compiled against say)
 *
 * ffgo's Go code reads FFmpeg structs at fixed offsets. Fields move between
 * FFmpeg majors, and some are removed (AVFrame.key_frame in FFmpeg 9), so
 * Go asks the shim -- compiled against the same headers as the libraries it
 * loads -- and audits its own values against the answers.
 * ============================================================================ */

#include <stddef.h>
#include <libavcodec/bsf.h>
#ifdef FFSHIM_HAVE_AVFILTER
#include <libavfilter/avfilter.h>
#endif
#include <libavutil/dict.h>

struct ffshim_field { const char *name; int offset; };

#define FFSHIM_FIELD(type, member) { #type "." #member, (int)offsetof(type, member) }

static const struct ffshim_field ffshim_fields[] = {
    /* AVFrame */
    FFSHIM_FIELD(AVFrame, data), FFSHIM_FIELD(AVFrame, linesize),
    FFSHIM_FIELD(AVFrame, extended_data), FFSHIM_FIELD(AVFrame, width),
    FFSHIM_FIELD(AVFrame, height), FFSHIM_FIELD(AVFrame, nb_samples),
    FFSHIM_FIELD(AVFrame, format), FFSHIM_FIELD(AVFrame, pts),
    FFSHIM_FIELD(AVFrame, sample_rate), FFSHIM_FIELD(AVFrame, buf),
    FFSHIM_FIELD(AVFrame, extended_buf), FFSHIM_FIELD(AVFrame, nb_extended_buf),
    FFSHIM_FIELD(AVFrame, flags), FFSHIM_FIELD(AVFrame, ch_layout.nb_channels),
    /* key_frame: deprecated in FFmpeg 6.1 (FF_API_FRAME_KEY), gone in 9.
       Before 6.1 the macro does not exist and the field does (libavutil
       below 59); after the removal neither exists. */
#if defined(FF_API_FRAME_KEY)
#  if FF_API_FRAME_KEY
    FFSHIM_FIELD(AVFrame, key_frame),
#  endif
#elif LIBAVUTIL_VERSION_MAJOR < 59
    FFSHIM_FIELD(AVFrame, key_frame),
#endif
    /* AVPacket */
    FFSHIM_FIELD(AVPacket, pts), FFSHIM_FIELD(AVPacket, dts), FFSHIM_FIELD(AVPacket, data),
    FFSHIM_FIELD(AVPacket, size), FFSHIM_FIELD(AVPacket, stream_index),
    FFSHIM_FIELD(AVPacket, flags), FFSHIM_FIELD(AVPacket, duration), FFSHIM_FIELD(AVPacket, pos),
    /* AVCodec */
    FFSHIM_FIELD(AVCodec, name), FFSHIM_FIELD(AVCodec, long_name),
    /* AVCodecContext */
    FFSHIM_FIELD(AVCodecContext, codec_type), FFSHIM_FIELD(AVCodecContext, codec_id),
    FFSHIM_FIELD(AVCodecContext, bit_rate), FFSHIM_FIELD(AVCodecContext, flags),
    FFSHIM_FIELD(AVCodecContext, time_base), FFSHIM_FIELD(AVCodecContext, width),
    FFSHIM_FIELD(AVCodecContext, height), FFSHIM_FIELD(AVCodecContext, gop_size),
    FFSHIM_FIELD(AVCodecContext, pix_fmt), FFSHIM_FIELD(AVCodecContext, max_b_frames),
    FFSHIM_FIELD(AVCodecContext, sample_rate), FFSHIM_FIELD(AVCodecContext, sample_fmt),
    FFSHIM_FIELD(AVCodecContext, frame_size), FFSHIM_FIELD(AVCodecContext, framerate),
    FFSHIM_FIELD(AVCodecContext, hw_frames_ctx), FFSHIM_FIELD(AVCodecContext, hw_device_ctx),
    FFSHIM_FIELD(AVCodecContext, ch_layout),
    /* AVCodecParameters */
    FFSHIM_FIELD(AVCodecParameters, codec_type), FFSHIM_FIELD(AVCodecParameters, codec_id),
    FFSHIM_FIELD(AVCodecParameters, codec_tag), FFSHIM_FIELD(AVCodecParameters, extradata),
    FFSHIM_FIELD(AVCodecParameters, extradata_size), FFSHIM_FIELD(AVCodecParameters, format),
    FFSHIM_FIELD(AVCodecParameters, width), FFSHIM_FIELD(AVCodecParameters, height),
    FFSHIM_FIELD(AVCodecParameters, sample_rate), FFSHIM_FIELD(AVCodecParameters, ch_layout.nb_channels),
    /* AVFormatContext */
    FFSHIM_FIELD(AVFormatContext, iformat), FFSHIM_FIELD(AVFormatContext, oformat),
    FFSHIM_FIELD(AVFormatContext, pb), FFSHIM_FIELD(AVFormatContext, nb_streams),
    FFSHIM_FIELD(AVFormatContext, streams), FFSHIM_FIELD(AVFormatContext, duration),
    FFSHIM_FIELD(AVFormatContext, bit_rate), FFSHIM_FIELD(AVFormatContext, flags),
    FFSHIM_FIELD(AVFormatContext, nb_programs), FFSHIM_FIELD(AVFormatContext, programs),
    FFSHIM_FIELD(AVFormatContext, nb_chapters), FFSHIM_FIELD(AVFormatContext, chapters),
    FFSHIM_FIELD(AVFormatContext, metadata), FFSHIM_FIELD(AVFormatContext, probe_score),
    /* AVInputFormat, AVOutputFormat */
    FFSHIM_FIELD(AVInputFormat, name), FFSHIM_FIELD(AVInputFormat, long_name),
    FFSHIM_FIELD(AVOutputFormat, flags),
    /* AVStream */
    FFSHIM_FIELD(AVStream, index), FFSHIM_FIELD(AVStream, id), FFSHIM_FIELD(AVStream, codecpar),
    FFSHIM_FIELD(AVStream, time_base), FFSHIM_FIELD(AVStream, metadata),
    FFSHIM_FIELD(AVStream, avg_frame_rate),
    /* AVChapter, AVProgram */
    FFSHIM_FIELD(AVChapter, id), FFSHIM_FIELD(AVChapter, time_base), FFSHIM_FIELD(AVChapter, start),
    FFSHIM_FIELD(AVChapter, end), FFSHIM_FIELD(AVChapter, metadata),
    FFSHIM_FIELD(AVProgram, id), FFSHIM_FIELD(AVProgram, stream_index),
    FFSHIM_FIELD(AVProgram, nb_stream_indexes), FFSHIM_FIELD(AVProgram, metadata),
    /* AVDictionaryEntry */
    FFSHIM_FIELD(AVDictionaryEntry, key), FFSHIM_FIELD(AVDictionaryEntry, value),
    /* AVSubtitle, AVSubtitleRect */
    FFSHIM_FIELD(AVSubtitle, format), FFSHIM_FIELD(AVSubtitle, start_display_time),
    FFSHIM_FIELD(AVSubtitle, end_display_time), FFSHIM_FIELD(AVSubtitle, num_rects),
    FFSHIM_FIELD(AVSubtitle, rects), FFSHIM_FIELD(AVSubtitle, pts),
    FFSHIM_FIELD(AVSubtitleRect, x), FFSHIM_FIELD(AVSubtitleRect, y),
    FFSHIM_FIELD(AVSubtitleRect, w), FFSHIM_FIELD(AVSubtitleRect, h),
    FFSHIM_FIELD(AVSubtitleRect, nb_colors), FFSHIM_FIELD(AVSubtitleRect, data[0]),
    FFSHIM_FIELD(AVSubtitleRect, data[1]), FFSHIM_FIELD(AVSubtitleRect, linesize[0]),
    FFSHIM_FIELD(AVSubtitleRect, type), FFSHIM_FIELD(AVSubtitleRect, text),
    FFSHIM_FIELD(AVSubtitleRect, ass),
    /* AVBSFContext */
    FFSHIM_FIELD(AVBSFContext, par_in), FFSHIM_FIELD(AVBSFContext, par_out),
    FFSHIM_FIELD(AVBSFContext, time_base_in), FFSHIM_FIELD(AVBSFContext, time_base_out),
#ifdef FFSHIM_HAVE_AVFILTER
    /* AVFilterInOut */
    FFSHIM_FIELD(AVFilterInOut, name), FFSHIM_FIELD(AVFilterInOut, filter_ctx),
    FFSHIM_FIELD(AVFilterInOut, pad_idx), FFSHIM_FIELD(AVFilterInOut, next),
#endif
};

int ffshim_offsetof(const char *field) {
    for (size_t i = 0; i < sizeof(ffshim_fields) / sizeof(ffshim_fields[0]); i++) {
        if (strcmp(ffshim_fields[i].name, field) == 0) {
            return ffshim_fields[i].offset;
        }
    }
    return -1;
}

/* The library majors of the headers this shim was compiled against:
   its offsets hold only for libraries of these majors. */
void ffshim_built_versions(int *avutil, int *avcodec, int *avformat) {
    *avutil = LIBAVUTIL_VERSION_MAJOR;
    *avcodec = LIBAVCODEC_VERSION_MAJOR;
    *avformat = LIBAVFORMAT_VERSION_MAJOR;
}

/* ============================================================================
 * HARDWARE FRAMES (GPU frames from a decoder through filters to an encoder)
 * ============================================================================ */

/* The GPU frame pool a decoded hardware frame belongs to. */
void *ffshim_frame_hw_frames_ctx(void *frame) {
    return ((AVFrame *)frame)->hw_frames_ctx;
}

#include <libavutil/hwcontext.h>

/* The hardware pixel format of a GPU frame pool (AV_PIX_FMT_CUDA,
   AV_PIX_FMT_VAAPI, AV_PIX_FMT_QSV, ...): what an encoder taking frames
   from it must be opened with. */
int ffshim_hwframes_format(void *frames_ref) {
    if (!frames_ref) return -1;
    AVBufferRef *ref = (AVBufferRef *)frames_ref;
    return (int)((AVHWFramesContext *)ref->data)->format;
}

#ifdef FFSHIM_HAVE_AVFILTER
#include <libavfilter/buffersrc.h>

/* Gives a buffersrc the GPU frame pool its input frames come from: a
   filter graph fed GPU frames negotiates nothing without it. The params
   take their own reference to frames_ref. */
int ffshim_buffersrc_set_hw_frames(void *src_ctx, void *frames_ref) {
    AVBufferSrcParameters *p = av_buffersrc_parameters_alloc();
    if (!p) return AVERROR(ENOMEM);
    p->format = -1; /* keep the format the buffer was created with */
    p->hw_frames_ctx = (AVBufferRef *)frames_ref;
    int ret = av_buffersrc_parameters_set((AVFilterContext *)src_ctx, p);
    av_free(p);
    return ret;
}
#endif /* FFSHIM_HAVE_AVFILTER */

/* ============================================================================
 * HEADER ENUM VALUES, STREAM DISPOSITION, CHANNEL LAYOUT NAMES
 * ============================================================================ */

struct ffshim_enum { const char *name; int value; };
#define FFSHIM_ENUM(e) { #e, (int)(e) }
static const struct ffshim_enum ffshim_enums[] = {
    FFSHIM_ENUM(AV_DISPOSITION_DEFAULT), FFSHIM_ENUM(AV_DISPOSITION_DUB),
    FFSHIM_ENUM(AV_DISPOSITION_ORIGINAL), FFSHIM_ENUM(AV_DISPOSITION_COMMENT),
    FFSHIM_ENUM(AV_DISPOSITION_LYRICS), FFSHIM_ENUM(AV_DISPOSITION_KARAOKE),
    FFSHIM_ENUM(AV_DISPOSITION_FORCED), FFSHIM_ENUM(AV_DISPOSITION_HEARING_IMPAIRED),
    FFSHIM_ENUM(AV_DISPOSITION_VISUAL_IMPAIRED), FFSHIM_ENUM(AV_DISPOSITION_CLEAN_EFFECTS),
    FFSHIM_ENUM(AV_DISPOSITION_ATTACHED_PIC), FFSHIM_ENUM(AV_DISPOSITION_CAPTIONS),
    FFSHIM_ENUM(AV_DISPOSITION_DESCRIPTIONS), FFSHIM_ENUM(AV_DISPOSITION_METADATA),
    FFSHIM_ENUM(AV_FRAME_DATA_MASTERING_DISPLAY_METADATA),
    FFSHIM_ENUM(AV_FRAME_DATA_CONTENT_LIGHT_LEVEL),
    FFSHIM_ENUM(AV_FRAME_DATA_DYNAMIC_HDR_PLUS),
#if LIBAVUTIL_VERSION_MAJOR >= 57
    FFSHIM_ENUM(AV_FRAME_DATA_DOVI_RPU_BUFFER),
    FFSHIM_ENUM(AV_FRAME_DATA_DOVI_METADATA),
#endif
    FFSHIM_ENUM(AV_PKT_DATA_MASTERING_DISPLAY_METADATA),
    FFSHIM_ENUM(AV_PKT_DATA_CONTENT_LIGHT_LEVEL),
    FFSHIM_ENUM(AV_PKT_DATA_DOVI_CONF),
#if LIBAVCODEC_VERSION_MAJOR >= 61
    FFSHIM_ENUM(AV_PKT_DATA_DYNAMIC_HDR10_PLUS),
#endif
};

/* An enum value as the headers this shim was compiled against define it. */
int ffshim_enum_value(const char *name, int *out) {
    for (size_t i = 0; i < sizeof(ffshim_enums) / sizeof(ffshim_enums[0]); i++) {
        if (strcmp(ffshim_enums[i].name, name) == 0) {
            *out = ffshim_enums[i].value;
            return 0;
        }
    }
    return -1;
}

int ffshim_stream_disposition(void *st) { return ((AVStream *)st)->disposition; }
void ffshim_stream_set_disposition(void *st, int d) { ((AVStream *)st)->disposition = d; }

/* A layout's name ("5.1(side)", "7.1"), as av_channel_layout_describe gives it. */
int ffshim_codecpar_ch_layout_describe(void *par, char *buf, size_t size) {
    return av_channel_layout_describe(&((AVCodecParameters *)par)->ch_layout, buf, size);
}
int ffshim_frame_ch_layout_describe(void *frame, char *buf, size_t size) {
    return av_channel_layout_describe(&((AVFrame *)frame)->ch_layout, buf, size);
}

/* ============================================================================
 * SIDE DATA (frames, stream codec parameters, encoder decoded side data)
 * ============================================================================ */

#include <libavcodec/packet.h>

/* A frame's side data of one type: its bytes and size, or NULL. */
void *ffshim_frame_side_data(void *frame, int type, size_t *size) {
    AVFrameSideData *sd = av_frame_get_side_data((AVFrame *)frame, (enum AVFrameSideDataType)type);
    if (!sd) return NULL;
    *size = sd->size;
    return sd->data;
}

/* Adds (a copy of) side data to a frame, replacing one of the same type. */
int ffshim_frame_add_side_data(void *frame, int type, const void *data, size_t size) {
    av_frame_remove_side_data((AVFrame *)frame, (enum AVFrameSideDataType)type);
    AVFrameSideData *sd = av_frame_new_side_data((AVFrame *)frame, (enum AVFrameSideDataType)type, size);
    if (!sd) return AVERROR(ENOMEM);
    if (size) memcpy(sd->data, data, size);
    return 0;
}

/* AVCodecParameters.coded_side_data arrived in libavcodec 60.31 (FFmpeg 6.1). */
#if LIBAVCODEC_VERSION_INT >= AV_VERSION_INT(60, 31, 100)
#define FFSHIM_HAVE_CODED_SIDE_DATA 1
#endif

int ffshim_codecpar_side_data_get(void *par, int type, void **data, size_t *size) {
#ifdef FFSHIM_HAVE_CODED_SIDE_DATA
    AVCodecParameters *p = par;
    const AVPacketSideData *sd = av_packet_side_data_get(p->coded_side_data, p->nb_coded_side_data,
                                                         (enum AVPacketSideDataType)type);
    if (!sd) return -1;
    *data = sd->data;
    *size = sd->size;
    return 0;
#else
    (void)par; (void)type; (void)data; (void)size;
    return AVERROR(ENOSYS);
#endif
}

/* Sets stream side data of one type (a copy), replacing one of that type. */
int ffshim_codecpar_side_data_set(void *par, int type, const void *data, size_t size) {
#ifdef FFSHIM_HAVE_CODED_SIDE_DATA
    AVCodecParameters *p = par;
    AVPacketSideData *sd = av_packet_side_data_new(&p->coded_side_data, &p->nb_coded_side_data,
                                                   (enum AVPacketSideDataType)type, size, 0);
    if (!sd) return AVERROR(ENOMEM);
    if (size) memcpy(sd->data, data, size);
    return 0;
#else
    (void)par; (void)type; (void)data; (void)size;
    return AVERROR(ENOSYS);
#endif
}

int ffshim_codecpar_nb_side_data(void *par) {
#ifdef FFSHIM_HAVE_CODED_SIDE_DATA
    return ((AVCodecParameters *)par)->nb_coded_side_data;
#else
    (void)par;
    return AVERROR(ENOSYS);
#endif
}

/* Side data an encoder reads before its first frame (mastering display,
   light level): AVCodecContext.decoded_side_data, libavcodec 61 (FFmpeg 7.0). */
int ffshim_codecctx_add_decoded_side_data(void *ctx, int type, const void *data, size_t size) {
#if LIBAVCODEC_VERSION_MAJOR >= 61
    AVCodecContext *c = ctx;
    AVFrameSideData *sd = av_frame_side_data_new(&c->decoded_side_data, &c->nb_decoded_side_data,
                                                 (enum AVFrameSideDataType)type, size,
                                                 AV_FRAME_SIDE_DATA_FLAG_REPLACE);
    if (!sd) return AVERROR(ENOMEM);
    if (size) memcpy(sd->data, data, size);
    return 0;
#else
    (void)ctx; (void)type; (void)data; (void)size;
    return AVERROR(ENOSYS);
#endif
}

/* ============================================================================
 * AUDIO: channel layouts by name, silence, an audio FIFO for fixed-size frames
 * ============================================================================ */

#include <libavutil/audio_fifo.h>
#include <libavutil/samplefmt.h>

int ffshim_ch_layout_nb_channels(const char *name) {
    AVChannelLayout l = {0};
    if (av_channel_layout_from_string(&l, name) < 0) return -1;
    int n = l.nb_channels;
    av_channel_layout_uninit(&l);
    return n;
}

int ffshim_codecctx_set_ch_layout(void *ctx, const char *name) {
    AVCodecContext *c = ctx;
    av_channel_layout_uninit(&c->ch_layout);
    return av_channel_layout_from_string(&c->ch_layout, name);
}

int ffshim_frame_set_ch_layout(void *frame, const char *name) {
    AVFrame *f = frame;
    av_channel_layout_uninit(&f->ch_layout);
    return av_channel_layout_from_string(&f->ch_layout, name);
}

int ffshim_frame_set_silence(void *frame) {
    AVFrame *f = frame;
    return av_samples_set_silence(f->extended_data, 0, f->nb_samples, f->ch_layout.nb_channels,
                                  (enum AVSampleFormat)f->format);
}

/* The encoder's first supported sample format, or -1 when it takes any. */
int ffshim_codec_first_sample_fmt(void *codec) {
#if LIBAVCODEC_VERSION_INT >= AV_VERSION_INT(61, 13, 100)
    const void *cfg = NULL;
    int n = 0;
    if (avcodec_get_supported_config(NULL, (const AVCodec *)codec, AV_CODEC_CONFIG_SAMPLE_FORMAT, 0, &cfg, &n) < 0 ||
        !cfg || n == 0)
        return -1;
    return ((const enum AVSampleFormat *)cfg)[0];
#else
    const enum AVSampleFormat *f = ((const AVCodec *)codec)->sample_fmts;
    return f ? f[0] : -1;
#endif
}

void *ffshim_audio_fifo_alloc(int fmt, int channels, int nb_samples) {
    return av_audio_fifo_alloc((enum AVSampleFormat)fmt, channels, nb_samples);
}
void ffshim_audio_fifo_free(void *fifo) { av_audio_fifo_free((AVAudioFifo *)fifo); }
int ffshim_audio_fifo_size(void *fifo) { return av_audio_fifo_size((AVAudioFifo *)fifo); }
int ffshim_audio_fifo_write_frame(void *fifo, void *frame) {
    AVFrame *f = frame;
    return av_audio_fifo_write((AVAudioFifo *)fifo, (void **)f->extended_data, f->nb_samples);
}
int ffshim_audio_fifo_read_frame(void *fifo, void *frame, int nb_samples) {
    AVFrame *f = frame;
    return av_audio_fifo_read((AVAudioFifo *)fifo, (void **)f->extended_data, nb_samples);
}

/* ============================================================================
 * ENCODERS, GPU FRAME POOLS, DEVICES ON FILTER GRAPHS
 * ============================================================================ */

int ffshim_codec_id(void *codec) { return codec ? (int)((const AVCodec *)codec)->id : -1; }

/* A GPU frame pool on device: format (AV_PIX_FMT_CUDA, _VAAPI, _QSV) holding
   sw_format surfaces of width x height, pool surfaces preallocated. */
int ffshim_hwframes_new(void *device_ref, int format, int sw_format, int width, int height, int pool,
                        void **out) {
    AVBufferRef *ref = av_hwframe_ctx_alloc((AVBufferRef *)device_ref);
    if (!ref) return AVERROR(ENOMEM);
    AVHWFramesContext *fc = (AVHWFramesContext *)ref->data;
    fc->format = (enum AVPixelFormat)format;
    fc->sw_format = (enum AVPixelFormat)sw_format;
    fc->width = width;
    fc->height = height;
    fc->initial_pool_size = pool;
    int ret = av_hwframe_ctx_init(ref);
    if (ret < 0) {
        av_buffer_unref(&ref);
        return ret;
    }
    *out = ref;
    return 0;
}

#ifdef FFSHIM_HAVE_AVFILTER
/* Gives a filter a hardware device (hwupload, vpp_qsv and scale_vaapi read
   it when the graph is configured). */
int ffshim_filter_set_hw_device(void *filter_ctx, void *device_ref) {
    AVFilterContext *f = filter_ctx;
    av_buffer_unref(&f->hw_device_ctx);
    f->hw_device_ctx = av_buffer_ref((AVBufferRef *)device_ref);
    return f->hw_device_ctx ? 0 : AVERROR(ENOMEM);
}
#endif

/* Clears a frame's picture type so an encoder chooses its own (fftools
   does the same before encoding a decoded frame). */
void ffshim_frame_set_pict_type_none(void *frame) {
    ((AVFrame *)frame)->pict_type = AV_PICTURE_TYPE_NONE;
}

/* A frame's channel count, from its AVChannelLayout. */
int ffshim_frame_nb_channels(void *frame) { return ((AVFrame *)frame)->ch_layout.nb_channels; }
