cmake_minimum_required(VERSION 3.24)
project(PhoneVRSteamVRDriver VERSION 1.0.0 LANGUAGES CXX)

if(NOT WIN32)
  message(FATAL_ERROR "The first PhoneVR SteamVR driver target is Windows x64 only")
endif()
if(CMAKE_SIZEOF_VOID_P AND NOT CMAKE_SIZEOF_VOID_P EQUAL 8)
  message(FATAL_ERROR "PhoneVR SteamVR driver requires a 64-bit build")
endif()

set(OPENVR_SDK_ROOT "" CACHE PATH "Path to a Valve OpenVR SDK checkout")
if(OPENVR_SDK_ROOT)
  set(OPENVR_INCLUDE_DIR "${OPENVR_SDK_ROOT}/headers")
else()
  include(FetchContent)
  set(FETCHCONTENT_QUIET OFF)
  FetchContent_Declare(openvr
    GIT_REPOSITORY https://github.com/ValveSoftware/openvr.git
    GIT_TAG 0924064316de3effbcd1acf1e309182a2deb1c05
    GIT_SHALLOW FALSE
  )
  if(POLICY CMP0169)
    cmake_policy(SET CMP0169 OLD)
  endif()
  FetchContent_GetProperties(openvr)
  if(NOT openvr_POPULATED)
    FetchContent_Populate(openvr)
  endif()
  set(OPENVR_INCLUDE_DIR "${openvr_SOURCE_DIR}/headers")
endif()
if(NOT EXISTS "${OPENVR_INCLUDE_DIR}/openvr_driver.h")
  message(FATAL_ERROR "OpenVR SDK header not found under ${OPENVR_INCLUDE_DIR}")
endif()

add_library(driver_phonevr SHARED src/driver.cpp)
target_compile_features(driver_phonevr PRIVATE cxx_std_17)
target_include_directories(driver_phonevr PRIVATE "${OPENVR_INCLUDE_DIR}")
target_compile_definitions(driver_phonevr PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_link_libraries(driver_phonevr PRIVATE ws2_32 d3d11 dxgi d3dcompiler)
set_target_properties(driver_phonevr PROPERTIES
  OUTPUT_NAME "driver_phonevr"
  RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/package/bin/win64"
  LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/package/bin/win64"
)

include(CTest)
if(BUILD_TESTING)
  add_executable(daydream_pose_model_test tests/daydream_pose_model_test.cpp)
  target_compile_features(daydream_pose_model_test PRIVATE cxx_std_17)
  add_test(NAME daydream_pose_model_test COMMAND daydream_pose_model_test)
endif()

configure_file(driver.vrdrivermanifest "${CMAKE_BINARY_DIR}/package/driver.vrdrivermanifest" COPYONLY)
install(TARGETS driver_phonevr RUNTIME DESTINATION bin/win64 LIBRARY DESTINATION bin/win64)
install(FILES driver.vrdrivermanifest DESTINATION .)
install(DIRECTORY resources/ DESTINATION resources OPTIONAL)
