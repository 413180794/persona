plugins {
    id("jarvis.jvm.library")
}

dependencies {
    api(project(":core:contracts"))
    api(libs.okhttp)
    testImplementation(libs.coroutines.test)
    testImplementation("com.squareup.okhttp3:mockwebserver:${libs.versions.okhttp.get()}")
    testImplementation("com.squareup.okhttp3:okhttp-tls:${libs.versions.okhttp.get()}")
}
