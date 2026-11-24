cmake_minimum_required(VERSION 3.21)
project(PhoneVRAudioBridge LANGUAGES CXX)

if(NOT WIN32)
    message(FATAL_ERROR "PhoneVRAudioBridge is a Windows-only WASAPI helper")
endif()

add_executable(PhoneVRAudioBridge PhoneVRAudioBridge.cpp)
target_compile_features(PhoneVRAudioBridge PRIVATE cxx_std_17)
target_compile_definitions(PhoneVRAudioBridge PRIVATE UNICODE _UNICODE WIN32_LEAN_AND_MEAN NOMINMAX WINVER=0x0A00 _WIN32_WINNT=0x0A00)
target_compile_options(PhoneVRAudioBridge PRIVATE /W4 /permissive- /EHsc /utf-8)
target_link_libraries(PhoneVRAudioBridge PRIVATE ole32 uuid avrt propsys advapi32)
set_target_properties(PhoneVRAudioBridge PROPERTIES
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin"
    RUNTIME_OUTPUT_DIRECTORY_DEBUG "${CMAKE_BINARY_DIR}/bin"
    RUNTIME_OUTPUT_DIRECTORY_RELEASE "${CMAKE_BINARY_DIR}/bin"
    RUNTIME_OUTPUT_DIRECTORY_RELWITHDEBINFO "${CMAKE_BINARY_DIR}/bin"
    RUNTIME_OUTPUT_DIRECTORY_MINSIZEREL "${CMAKE_BINARY_DIR}/bin"
)
