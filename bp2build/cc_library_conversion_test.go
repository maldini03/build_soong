// Copyright 2021 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bp2build

import (
	"android/soong/android"
	"android/soong/cc"
	"strings"
	"testing"
)

const (
	// See cc/testing.go for more context
	soongCcLibraryPreamble = `
cc_defaults {
	name: "linux_bionic_supported",
}

toolchain_library {
	name: "libclang_rt.builtins-x86_64-android",
	defaults: ["linux_bionic_supported"],
	vendor_available: true,
	vendor_ramdisk_available: true,
	product_available: true,
	recovery_available: true,
	native_bridge_supported: true,
	src: "",
}`
)

func runCcLibraryTestCase(t *testing.T, tc bp2buildTestCase) {
	t.Helper()
	runBp2BuildTestCase(t, registerCcLibraryModuleTypes, tc)
}

func registerCcLibraryModuleTypes(ctx android.RegistrationContext) {
	cc.RegisterCCBuildComponents(ctx)
	ctx.RegisterModuleType("cc_library_static", cc.LibraryStaticFactory)
	ctx.RegisterModuleType("toolchain_library", cc.ToolchainLibraryFactory)
	ctx.RegisterModuleType("cc_library_headers", cc.LibraryHeaderFactory)
}

func runBp2BuildTestCase(t *testing.T, registerModuleTypes func(ctx android.RegistrationContext), tc bp2buildTestCase) {
	t.Helper()
	dir := "."
	filesystem := make(map[string][]byte)
	toParse := []string{
		"Android.bp",
	}
	for f, content := range tc.filesystem {
		if strings.HasSuffix(f, "Android.bp") {
			toParse = append(toParse, f)
		}
		filesystem[f] = []byte(content)
	}
	config := android.TestConfig(buildDir, nil, tc.blueprint, filesystem)
	ctx := android.NewTestContext(config)

	registerModuleTypes(ctx)
	ctx.RegisterModuleType(tc.moduleTypeUnderTest, tc.moduleTypeUnderTestFactory)
	ctx.RegisterBp2BuildConfig(bp2buildConfig)
	for _, m := range tc.depsMutators {
		ctx.DepsBp2BuildMutators(m)
	}
	ctx.RegisterBp2BuildMutator(tc.moduleTypeUnderTest, tc.moduleTypeUnderTestBp2BuildMutator)
	ctx.RegisterForBazelConversion()

	_, errs := ctx.ParseFileList(dir, toParse)
	if errored(t, tc.description, errs) {
		return
	}
	_, errs = ctx.ResolveDependencies(config)
	if errored(t, tc.description, errs) {
		return
	}

	checkDir := dir
	if tc.dir != "" {
		checkDir = tc.dir
	}
	codegenCtx := NewCodegenContext(config, *ctx.Context, Bp2Build)
	bazelTargets := generateBazelTargetsForDir(codegenCtx, checkDir)
	if actualCount, expectedCount := len(bazelTargets), len(tc.expectedBazelTargets); actualCount != expectedCount {
		t.Errorf("%s: Expected %d bazel target, got %d", tc.description, expectedCount, actualCount)
	} else {
		for i, target := range bazelTargets {
			if w, g := tc.expectedBazelTargets[i], target.content; w != g {
				t.Errorf(
					"%s: Expected generated Bazel target to be '%s', got '%s'",
					tc.description,
					w,
					g,
				)
			}
		}
	}
}

func TestCcLibrarySimple(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library - simple example",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		filesystem: map[string]string{
			"android.cpp": "",
			"darwin.cpp":  "",
			// Refer to cc.headerExts for the supported header extensions in Soong.
			"header.h":         "",
			"header.hh":        "",
			"header.hpp":       "",
			"header.hxx":       "",
			"header.h++":       "",
			"header.inl":       "",
			"header.inc":       "",
			"header.ipp":       "",
			"header.h.generic": "",
			"impl.cpp":         "",
			"linux.cpp":        "",
			"x86.cpp":          "",
			"x86_64.cpp":       "",
			"foo-dir/a.h":      "",
		},
		blueprint: soongCcLibraryPreamble + `
cc_library_headers { name: "some-headers" }
cc_library {
    name: "foo-lib",
    srcs: ["impl.cpp"],
    cflags: ["-Wall"],
    header_libs: ["some-headers"],
    export_include_dirs: ["foo-dir"],
    ldflags: ["-Wl,--exclude-libs=bar.a"],
    arch: {
        x86: {
            ldflags: ["-Wl,--exclude-libs=baz.a"],
            srcs: ["x86.cpp"],
        },
        x86_64: {
            ldflags: ["-Wl,--exclude-libs=qux.a"],
            srcs: ["x86_64.cpp"],
        },
    },
    target: {
        android: {
            srcs: ["android.cpp"],
        },
        linux_glibc: {
            srcs: ["linux.cpp"],
        },
        darwin: {
            srcs: ["darwin.cpp"],
        },
    },
}
`,
		expectedBazelTargets: []string{`cc_library(
    name = "foo-lib",
    copts = [
        "-Wall",
        "-I.",
        "-I$(BINDIR)/.",
    ],
    implementation_deps = [":some-headers"],
    includes = ["foo-dir"],
    linkopts = ["-Wl,--exclude-libs=bar.a"] + select({
        "//build/bazel/platforms/arch:x86": ["-Wl,--exclude-libs=baz.a"],
        "//build/bazel/platforms/arch:x86_64": ["-Wl,--exclude-libs=qux.a"],
        "//conditions:default": [],
    }),
    srcs = ["impl.cpp"] + select({
        "//build/bazel/platforms/arch:x86": ["x86.cpp"],
        "//build/bazel/platforms/arch:x86_64": ["x86_64.cpp"],
        "//conditions:default": [],
    }) + select({
        "//build/bazel/platforms/os:android": ["android.cpp"],
        "//build/bazel/platforms/os:darwin": ["darwin.cpp"],
        "//build/bazel/platforms/os:linux": ["linux.cpp"],
        "//conditions:default": [],
    }),
)`}})
}

func TestCcLibraryTrimmedLdAndroid(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library - trimmed example of //bionic/linker:ld-android",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		filesystem: map[string]string{
			"ld-android.cpp":           "",
			"linked_list.h":            "",
			"linker.h":                 "",
			"linker_block_allocator.h": "",
			"linker_cfi.h":             "",
		},
		blueprint: soongCcLibraryPreamble + `
cc_library_headers { name: "libc_headers" }
cc_library {
    name: "fake-ld-android",
    srcs: ["ld_android.cpp"],
    cflags: [
        "-Wall",
        "-Wextra",
        "-Wunused",
        "-Werror",
    ],
    header_libs: ["libc_headers"],
    ldflags: [
        "-Wl,--exclude-libs=libgcc.a",
        "-Wl,--exclude-libs=libgcc_stripped.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-arm-android.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-aarch64-android.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-i686-android.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-x86_64-android.a",
    ],
    arch: {
        x86: {
            ldflags: ["-Wl,--exclude-libs=libgcc_eh.a"],
        },
        x86_64: {
            ldflags: ["-Wl,--exclude-libs=libgcc_eh.a"],
        },
    },
}
`,
		expectedBazelTargets: []string{`cc_library(
    name = "fake-ld-android",
    copts = [
        "-Wall",
        "-Wextra",
        "-Wunused",
        "-Werror",
        "-I.",
        "-I$(BINDIR)/.",
    ],
    implementation_deps = [":libc_headers"],
    linkopts = [
        "-Wl,--exclude-libs=libgcc.a",
        "-Wl,--exclude-libs=libgcc_stripped.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-arm-android.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-aarch64-android.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-i686-android.a",
        "-Wl,--exclude-libs=libclang_rt.builtins-x86_64-android.a",
    ] + select({
        "//build/bazel/platforms/arch:x86": ["-Wl,--exclude-libs=libgcc_eh.a"],
        "//build/bazel/platforms/arch:x86_64": ["-Wl,--exclude-libs=libgcc_eh.a"],
        "//conditions:default": [],
    }),
    srcs = ["ld_android.cpp"],
)`},
	})
}

func TestCcLibraryExcludeSrcs(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library exclude_srcs - trimmed example of //external/arm-optimized-routines:libarm-optimized-routines-math",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		dir:                                "external",
		filesystem: map[string]string{
			"external/math/cosf.c":      "",
			"external/math/erf.c":       "",
			"external/math/erf_data.c":  "",
			"external/math/erff.c":      "",
			"external/math/erff_data.c": "",
			"external/Android.bp": `
cc_library {
    name: "fake-libarm-optimized-routines-math",
    exclude_srcs: [
        // Provided by:
        // bionic/libm/upstream-freebsd/lib/msun/src/s_erf.c
        // bionic/libm/upstream-freebsd/lib/msun/src/s_erff.c
        "math/erf.c",
        "math/erf_data.c",
        "math/erff.c",
        "math/erff_data.c",
    ],
    srcs: [
        "math/*.c",
    ],
    // arch-specific settings
    arch: {
        arm64: {
            cflags: [
                "-DHAVE_FAST_FMA=1",
            ],
        },
    },
    bazel_module: { bp2build_available: true },
}
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "fake-libarm-optimized-routines-math",
    copts = [
        "-Iexternal",
        "-I$(BINDIR)/external",
    ] + select({
        "//build/bazel/platforms/arch:arm64": ["-DHAVE_FAST_FMA=1"],
        "//conditions:default": [],
    }),
    srcs_c = ["math/cosf.c"],
)`},
	})
}

func TestCcLibrarySharedStaticProps(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library shared/static props",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/both.cpp":       "",
			"foo/bar/sharedonly.cpp": "",
			"foo/bar/staticonly.cpp": "",
			"foo/bar/Android.bp": `
cc_library {
    name: "a",
    srcs: ["both.cpp"],
    cflags: ["bothflag"],
    shared_libs: ["shared_dep_for_both"],
    static_libs: ["static_dep_for_both"],
    whole_static_libs: ["whole_static_lib_for_both"],
    static: {
        srcs: ["staticonly.cpp"],
        cflags: ["staticflag"],
        shared_libs: ["shared_dep_for_static"],
        static_libs: ["static_dep_for_static"],
        whole_static_libs: ["whole_static_lib_for_static"],
    },
    shared: {
        srcs: ["sharedonly.cpp"],
        cflags: ["sharedflag"],
        shared_libs: ["shared_dep_for_shared"],
        static_libs: ["static_dep_for_shared"],
        whole_static_libs: ["whole_static_lib_for_shared"],
    },
    bazel_module: { bp2build_available: true },
}

cc_library_static { name: "static_dep_for_shared" }

cc_library_static { name: "static_dep_for_static" }

cc_library_static { name: "static_dep_for_both" }

cc_library_static { name: "whole_static_lib_for_shared" }

cc_library_static { name: "whole_static_lib_for_static" }

cc_library_static { name: "whole_static_lib_for_both" }

cc_library { name: "shared_dep_for_shared" }

cc_library { name: "shared_dep_for_static" }

cc_library { name: "shared_dep_for_both" }
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "bothflag",
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    dynamic_deps = [":shared_dep_for_both"],
    dynamic_deps_for_shared = [":shared_dep_for_shared"],
    dynamic_deps_for_static = [":shared_dep_for_static"],
    implementation_deps = [":static_dep_for_both"],
    shared_copts = ["sharedflag"],
    shared_srcs = ["sharedonly.cpp"],
    srcs = ["both.cpp"],
    static_copts = ["staticflag"],
    static_deps_for_shared = [":static_dep_for_shared"],
    static_deps_for_static = [":static_dep_for_static"],
    static_srcs = ["staticonly.cpp"],
    whole_archive_deps = [":whole_static_lib_for_both"],
    whole_archive_deps_for_shared = [":whole_static_lib_for_shared"],
    whole_archive_deps_for_static = [":whole_static_lib_for_static"],
)`},
	})
}

func TestCcLibrarySharedStaticPropsInArch(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library shared/static props in arch",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/arm.cpp":        "",
			"foo/bar/x86.cpp":        "",
			"foo/bar/sharedonly.cpp": "",
			"foo/bar/staticonly.cpp": "",
			"foo/bar/Android.bp": `
cc_library {
    name: "a",
    arch: {
        arm: {
            shared: {
                srcs: ["arm_shared.cpp"],
                cflags: ["-DARM_SHARED"],
                static_libs: ["arm_static_dep_for_shared"],
                whole_static_libs: ["arm_whole_static_dep_for_shared"],
                shared_libs: ["arm_shared_dep_for_shared"],
            },
        },
        x86: {
            static: {
                srcs: ["x86_static.cpp"],
                cflags: ["-DX86_STATIC"],
                static_libs: ["x86_dep_for_static"],
            },
        },
    },
    target: {
        android: {
            shared: {
                srcs: ["android_shared.cpp"],
                cflags: ["-DANDROID_SHARED"],
                static_libs: ["android_dep_for_shared"],
            },
        },
        android_arm: {
            shared: {
                cflags: ["-DANDROID_ARM_SHARED"],
            },
        },
    },
    srcs: ["both.cpp"],
    cflags: ["bothflag"],
    static_libs: ["static_dep_for_both"],
    static: {
        srcs: ["staticonly.cpp"],
        cflags: ["staticflag"],
        static_libs: ["static_dep_for_static"],
    },
    shared: {
        srcs: ["sharedonly.cpp"],
        cflags: ["sharedflag"],
        static_libs: ["static_dep_for_shared"],
    },
    bazel_module: { bp2build_available: true },
}

cc_library_static { name: "static_dep_for_shared" }
cc_library_static { name: "static_dep_for_static" }
cc_library_static { name: "static_dep_for_both" }

cc_library_static { name: "arm_static_dep_for_shared" }
cc_library_static { name: "arm_whole_static_dep_for_shared" }
cc_library_static { name: "arm_shared_dep_for_shared" }

cc_library_static { name: "x86_dep_for_static" }

cc_library_static { name: "android_dep_for_shared" }
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "bothflag",
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    dynamic_deps_for_shared = select({
        "//build/bazel/platforms/arch:arm": [":arm_shared_dep_for_shared"],
        "//conditions:default": [],
    }),
    implementation_deps = [":static_dep_for_both"],
    shared_copts = ["sharedflag"] + select({
        "//build/bazel/platforms/arch:arm": ["-DARM_SHARED"],
        "//conditions:default": [],
    }) + select({
        "//build/bazel/platforms/os:android": ["-DANDROID_SHARED"],
        "//conditions:default": [],
    }) + select({
        "//build/bazel/platforms:android_arm": ["-DANDROID_ARM_SHARED"],
        "//conditions:default": [],
    }),
    shared_srcs = ["sharedonly.cpp"] + select({
        "//build/bazel/platforms/arch:arm": ["arm_shared.cpp"],
        "//conditions:default": [],
    }) + select({
        "//build/bazel/platforms/os:android": ["android_shared.cpp"],
        "//conditions:default": [],
    }),
    srcs = ["both.cpp"],
    static_copts = ["staticflag"] + select({
        "//build/bazel/platforms/arch:x86": ["-DX86_STATIC"],
        "//conditions:default": [],
    }),
    static_deps_for_shared = [":static_dep_for_shared"] + select({
        "//build/bazel/platforms/arch:arm": [":arm_static_dep_for_shared"],
        "//conditions:default": [],
    }) + select({
        "//build/bazel/platforms/os:android": [":android_dep_for_shared"],
        "//conditions:default": [],
    }),
    static_deps_for_static = [":static_dep_for_static"] + select({
        "//build/bazel/platforms/arch:x86": [":x86_dep_for_static"],
        "//conditions:default": [],
    }),
    static_srcs = ["staticonly.cpp"] + select({
        "//build/bazel/platforms/arch:x86": ["x86_static.cpp"],
        "//conditions:default": [],
    }),
    whole_archive_deps_for_shared = select({
        "//build/bazel/platforms/arch:arm": [":arm_whole_static_dep_for_shared"],
        "//conditions:default": [],
    }),
)`},
	})
}

func TestCcLibraryNonConfiguredVersionScript(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library non-configured version script",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/Android.bp": `
cc_library {
    name: "a",
    srcs: ["a.cpp"],
    version_script: "v.map",
    bazel_module: { bp2build_available: true },
}
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    srcs = ["a.cpp"],
    version_script = "v.map",
)`},
	})
}

func TestCcLibraryConfiguredVersionScript(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library configured version script",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/Android.bp": `
		cc_library {
		   name: "a",
		   srcs: ["a.cpp"],
		   arch: {
		     arm: {
		       version_script: "arm.map",
		     },
		     arm64: {
		       version_script: "arm64.map",
		     },
		   },

		   bazel_module: { bp2build_available: true },
		}
		`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    srcs = ["a.cpp"],
    version_script = select({
        "//build/bazel/platforms/arch:arm": "arm.map",
        "//build/bazel/platforms/arch:arm64": "arm64.map",
        "//conditions:default": None,
    }),
)`},
	})
}

func TestCcLibrarySharedLibs(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library shared_libs",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/Android.bp": `
cc_library {
    name: "mylib",
    bazel_module: { bp2build_available: true },
}

cc_library {
    name: "a",
    shared_libs: ["mylib",],
    bazel_module: { bp2build_available: true },
}
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    dynamic_deps = [":mylib"],
)`, `cc_library(
    name = "mylib",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
)`},
	})
}

func TestCcLibraryPackRelocations(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library pack_relocations test",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/Android.bp": `
cc_library {
    name: "a",
    srcs: ["a.cpp"],
    pack_relocations: false,
    bazel_module: { bp2build_available: true },
}

cc_library {
    name: "b",
    srcs: ["b.cpp"],
    arch: {
        x86_64: {
		pack_relocations: false,
	},
    },
    bazel_module: { bp2build_available: true },
}

cc_library {
    name: "c",
    srcs: ["c.cpp"],
    target: {
        darwin: {
		pack_relocations: false,
	},
    },
    bazel_module: { bp2build_available: true },
}`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    linkopts = ["-Wl,--pack-dyn-relocs=none"],
    srcs = ["a.cpp"],
)`, `cc_library(
    name = "b",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    linkopts = select({
        "//build/bazel/platforms/arch:x86_64": ["-Wl,--pack-dyn-relocs=none"],
        "//conditions:default": [],
    }),
    srcs = ["b.cpp"],
)`, `cc_library(
    name = "c",
    copts = [
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    linkopts = select({
        "//build/bazel/platforms/os:darwin": ["-Wl,--pack-dyn-relocs=none"],
        "//conditions:default": [],
    }),
    srcs = ["c.cpp"],
)`},
	})
}

func TestCcLibrarySpacesInCopts(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library spaces in copts",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/Android.bp": `
cc_library {
    name: "a",
    cflags: ["-include header.h",],
    bazel_module: { bp2build_available: true },
}
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "-include",
        "header.h",
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
)`},
	})
}

func TestCcLibraryCppFlagsGoesIntoCopts(t *testing.T) {
	runCcLibraryTestCase(t, bp2buildTestCase{
		description:                        "cc_library cppflags usage",
		moduleTypeUnderTest:                "cc_library",
		moduleTypeUnderTestFactory:         cc.LibraryFactory,
		moduleTypeUnderTestBp2BuildMutator: cc.CcLibraryBp2Build,
		depsMutators:                       []android.RegisterMutatorFunc{cc.RegisterDepsBp2Build},
		dir:                                "foo/bar",
		filesystem: map[string]string{
			"foo/bar/Android.bp": `cc_library {
    name: "a",
    srcs: ["a.cpp"],
    cflags: [
		"-Wall",
	],
    cppflags: [
        "-fsigned-char",
        "-pedantic",
	],
    arch: {
        arm64: {
            cppflags: ["-DARM64=1"],
		},
	},
    target: {
        android: {
            cppflags: ["-DANDROID=1"],
		},
	},
    bazel_module: { bp2build_available: true  },
}
`,
		},
		blueprint: soongCcLibraryPreamble,
		expectedBazelTargets: []string{`cc_library(
    name = "a",
    copts = [
        "-Wall",
        "-Ifoo/bar",
        "-I$(BINDIR)/foo/bar",
    ],
    cppflags = [
        "-fsigned-char",
        "-pedantic",
    ] + select({
        "//build/bazel/platforms/arch:arm64": ["-DARM64=1"],
        "//conditions:default": [],
    }) + select({
        "//build/bazel/platforms/os:android": ["-DANDROID=1"],
        "//conditions:default": [],
    }),
    srcs = ["a.cpp"],
)`},
	})
}
