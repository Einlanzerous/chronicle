# syntax=docker/dockerfile:1

# Shared estate ASR runner — whisper.cpp on the R9700 (gfx1201, RDNA4), Vulkan/RADV.
# CHRN-24. Chronicle is client one; Catenary is client two (CHRN-29).
#
#   docker build -f deploy/asr/Dockerfile -t estate-asr:dev deploy/asr
#
# Everything that enters a published number is pinned, and the pins are the
# deliverable rather than housekeeping. IDEA-26 could not promise that a later
# whisper.cpp keeps this backend fast, and this rig has now produced a
# confidently wrong number twice (a slow backend that looked correct, then a
# decode flag that looked like formatting). A float in any of the five values
# below moves a published latency figure with nothing in the output to say so.
#
# The base tag is the exception and is deliberately NOT one of the five:
# `ubuntu:24.04` is rebuilt regularly and is not a fixed input. That is only
# acceptable because every component of it that reaches a measurement is pinned
# individually below — ffmpeg above all, since the decode is counted in every
# figure and a base-image bump would otherwise move `decode_ms` against a gate
# whose whole margin is a few percent.
ARG UBUNTU=24.04

# whisper.cpp 1fe009ca (2026-08-14) / ggml 0.20.0 — the exact tree IDEA-26 built
# and CHRN-12 re-measured. Not a tag: tags move.
ARG WHISPER_REF=1fe009caeda75f69bc864d6370b10674e45a92bd

# LunarG SDK, and not a convenience. Noble ships glslc 2023.8, three years
# behind this, and it cannot compile ggml's KHR_cooperative_matrix shaders —
# the path that makes RDNA4 fast, reported by ggml as "matrix cores:
# KHR_coopmat". Building against distro shaderc yields a binary that runs,
# transcribes correctly, and is well off the pace: precisely the failure IDEA-26
# names and the one nothing about the output reveals.
ARG VULKAN_SDK_VERSION=1.4.357.1

# RADV. Pinned to the driver the reference numbers were taken on, and pinned
# EXACTLY on purpose: when this version rotates out of noble-updates the build
# fails loudly instead of quietly measuring a different driver. Bump it as a
# decision, then re-measure and update deploy/asr/README.md.
ARG MESA_VERSION=25.2.8-0ubuntu0.24.04.2

# The Vulkan loader. Declared once here and inherited by both stages that need
# it, so the two cannot drift apart and leave the README's single-row claim
# true of only one of them.
ARG LIBVULKAN_VERSION=1.3.275.0-1build1

# ffmpeg, which is in a measurement rather than merely near one: every figure
# this image is compared against counts an Opus decode, so an ffmpeg that
# arrives with whatever the base tag happens to hold that month is a moving
# part inside the gate.
ARG FFMPEG_VERSION=7:6.1.1-3ubuntu5


# ---------------------------------------------------------------------------
# 1) Vulkan SDK — headers and glslc for the build stage only.
# ---------------------------------------------------------------------------
FROM ubuntu:${UBUNTU} AS sdk
ARG VULKAN_SDK_VERSION
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl xz-utils \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /tmp/vulkan_sdk.tar.xz \
        "https://sdk.lunarg.com/sdk/download/${VULKAN_SDK_VERSION}/linux/vulkan_sdk.tar.xz" \
    && mkdir -p /opt/sdk \
    && tar -xf /tmp/vulkan_sdk.tar.xz -C /opt/sdk \
    && mv "/opt/sdk/${VULKAN_SDK_VERSION}/x86_64" /opt/vulkan-sdk \
    && rm -rf /tmp/vulkan_sdk.tar.xz /opt/sdk


# ---------------------------------------------------------------------------
# 2) Build whisper.cpp with GGML_VULKAN=1, from the pinned commit.
# ---------------------------------------------------------------------------
FROM ubuntu:${UBUNTU} AS build
ARG WHISPER_REF
ARG LIBVULKAN_VERSION
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential cmake git ca-certificates \
        "libvulkan1=${LIBVULKAN_VERSION}" \
    && rm -rf /var/lib/apt/lists/*

COPY --from=sdk /opt/vulkan-sdk /opt/vulkan-sdk
ENV VULKAN_SDK=/opt/vulkan-sdk

# Fetch the one commit rather than the history. --depth 1 on a bare init keeps
# this honest: the SHA is the input, so there is no branch to drift under it.
RUN git init /src/whisper.cpp \
    && cd /src/whisper.cpp \
    && git remote add origin https://github.com/ggml-org/whisper.cpp.git \
    && git fetch --depth 1 origin "${WHISPER_REF}" \
    && git checkout FETCH_HEAD

# IDEA-26's hand-made libvulkan.so symlink, reproduced rather than skipped.
#
# The write-up attributes it to the host having no libvulkan-dev and no sudo,
# which is true but not the whole reason. The SDK does ship a loader — under
# lib/VulkanLoader/lib/, NOT lib/ — so $VULKAN_SDK/lib carries no libvulkan.so
# of any kind and there is nothing there to link against. Assuming otherwise
# fails the build with "No rule to make target .../lib/libvulkan.so".
#
# The consequence runs the other way too, and matters more: bench.sh puts
# $VULKAN_SDK/lib on LD_LIBRARY_PATH, and that directory holds no loader — so
# every reference number in CHRN-12 was measured against the DISTRO loader,
# 1.3.275.0, with the SDK supplying headers and glslc only. That is what is
# pinned here, because it is what was measured.
RUN mkdir -p /opt/vklib \
    && ln -s /usr/lib/x86_64-linux-gnu/libvulkan.so.1 /opt/vklib/libvulkan.so

RUN cmake -S /src/whisper.cpp -B /src/whisper.cpp/build-vk \
        -DCMAKE_BUILD_TYPE=Release \
        -DGGML_VULKAN=1 \
        -DVulkan_INCLUDE_DIR="${VULKAN_SDK}/include" \
        -DVulkan_LIBRARY=/opt/vklib/libvulkan.so \
        -DVulkan_GLSLC_EXECUTABLE="${VULKAN_SDK}/bin/glslc" \
    && cmake --build /src/whisper.cpp/build-vk --config Release -j "$(nproc)"


# ---------------------------------------------------------------------------
# 3) Runtime.
# ---------------------------------------------------------------------------
FROM ubuntu:${UBUNTU} AS runtime
ARG MESA_VERSION
ARG LIBVULKAN_VERSION
ARG FFMPEG_VERSION
ARG WHISPER_REF
ARG VULKAN_SDK_VERSION

# The pins, carried on the image so a measurement can record what produced it
# without a script re-stating values that would then drift from these. The whole
# argument above is that a moved pin must not be able to move a published figure
# silently; a CSV that cannot name its own pin set has that hole in it.
LABEL org.opencontainers.image.title="estate-asr" \
      org.opencontainers.image.description="whisper.cpp on the R9700 (gfx1201) via Vulkan/RADV — shared estate ASR runner, CHRN-24" \
      estate.asr.whisper_ref="${WHISPER_REF}" \
      estate.asr.vulkan_sdk="${VULKAN_SDK_VERSION}" \
      estate.asr.mesa="${MESA_VERSION}" \
      estate.asr.libvulkan="${LIBVULKAN_VERSION}" \
      estate.asr.ffmpeg="${FFMPEG_VERSION}"

# ffmpeg is here because the job contract accepts compressed audio and decodes
# it — decided on the epic 2026-08-27, after CHRN-21 established that Chronicle
# ships no decoder at all. whisper.cpp does not read Opus, so without ffmpeg in
# this image nothing in the estate can turn a memo into a transcript.
#
# It is also why the measured totals are reproducible here: every figure counts
# a 149 ms decode, so an image that cannot decode cannot be compared to them.
#
# bsdextrautils is for column(1), which the benchmark harness prints through.
RUN apt-get update && apt-get install -y --no-install-recommends \
        "mesa-vulkan-drivers=${MESA_VERSION}" \
        "libvulkan1=${LIBVULKAN_VERSION}" \
        libgomp1 \
        "ffmpeg=${FFMPEG_VERSION}" \
        bsdextrautils \
    && rm -rf /var/lib/apt/lists/*

# Laid out as $WHISPER/build-vk/bin and $WHISPER/models on purpose: it is the
# tree bench.sh already expects, so the harness that produced the reference
# numbers runs inside this image unmodified. A benchmark rewritten for the
# container is a benchmark that no longer compares to anything.
COPY --from=build /src/whisper.cpp/build-vk/bin /opt/whisper/build-vk/bin

# No VULKAN_SDK here: the SDK is a build-time dependency only. The loader is
# the distro's, pinned above, and RADV is found through the ICD manifest that
# mesa-vulkan-drivers installs — neither needs the SDK on the path.
ENV WHISPER=/opt/whisper \
    LD_LIBRARY_PATH=/opt/whisper/build-vk/bin \
    GGML_VK_VISIBLE_DEVICES=0

# The model store is a mount, never a layer. large-v3 alone is 2.9 GB and the
# four together are 5.0 GB, against a 466 MB default — and a model is data the
# service is pointed at, not code it ships.
VOLUME ["/opt/whisper/models"]

RUN ln -s /opt/whisper/build-vk/bin/whisper-cli /usr/local/bin/whisper-cli
ENTRYPOINT ["/usr/local/bin/whisper-cli"]
