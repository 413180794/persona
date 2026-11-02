package jarvis.android.ui.media

import android.Manifest
import android.graphics.Bitmap
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import jarvis.android.data.ImagePayload
import jarvis.android.data.Hub
import jarvis.android.ui.JTheme
import jarvis.android.ui.JarvisTheme
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.surface
import kotlinx.coroutines.launch
import kotlin.math.abs

private sealed interface Page {
    data object Loading : Page
    data object Failed : Page
    data class Ready(val bitmap: Bitmap) : Page
}

/**
 * Full-screen images (iOS `ImageViewer`): swipe through [ImageViewerArgs.images] from its index, pinch / double-tap
 * zoom and pan, a tap toggles the controls, pull down to close. Top: close and 「n / m」; bottom: 保存 (to the
 * gallery), 编辑 (only with [ImageViewerArgs.canEdit]: the marked-up picture goes to [onEdited], the chat's composer), 分享, 拷贝.
 * Always in the dark appearance (iOS's black viewer): the controls are the design system's, light on the black.
 */
@Composable
fun ImageViewerScreen(hub: Hub, args: ImageViewerArgs, onBack: () -> Unit, onEdited: (ImagePayload) -> Unit = {}) {
    val images = args.images
    if (images.isEmpty()) {
        LaunchedEffect(Unit) { onBack() }
        return
    }
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val pager = rememberPagerState(initialPage = args.index.coerceIn(0, images.lastIndex)) { images.size }
    val zoomed = remember { mutableStateMapOf<Int, Boolean>() }
    val ready = remember { mutableStateMapOf<Int, Bitmap>() }
    var chrome by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<Bitmap?>(null) }
    var pull by remember { mutableFloatStateOf(0f) }
    val fade = (1f - abs(pull) / 900f).coerceIn(0.3f, 1f)
    BackHandler(onBack = onBack)

    fun current(): ViewerImage = images[pager.currentPage]

    fun save() {
        saving = true
        scope.launch {
            val img = current()
            val bytes = ChatImageLoader.bytes(hub, img)
            val ok = bytes != null && MediaShare.saveImage(ctx, bytes, img.mime ?: MediaShare.sniffMime(bytes))
            saving = false
            MediaShare.toast(ctx, if (ok) "已保存到相册" else "保存失败")
        }
    }
    // Android 8–9 need the storage permission to write into Pictures; 10+ go through MediaStore without one.
    val askStorage = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) save() else MediaShare.toast(ctx, "没有存储权限，保存不了")
    }

    JarvisTheme(dark = true) {
    Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = fade))) {
        HorizontalPager(
            state = pager,
            userScrollEnabled = zoomed[pager.currentPage] != true,
            modifier = Modifier.fillMaxSize(),
            beyondViewportPageCount = 1,
        ) { page ->
            var attempt by remember { mutableIntStateOf(0) }
            val state by produceState<Page>(
                ChatImageLoader.cached(images[page], 0)?.let { Page.Ready(it) } ?: Page.Loading, images[page], attempt,
            ) {
                value = ChatImageLoader.cached(images[page], 0)?.let { Page.Ready(it) } ?: Page.Loading
                if (value !is Page.Ready) {
                    value = ChatImageLoader.load(hub, images[page], 0)?.let { Page.Ready(it) } ?: Page.Failed
                }
                (value as? Page.Ready)?.let { ready[page] = it.bitmap }
            }
            when (val s = state) {
                Page.Failed -> Column(
                    Modifier.fillMaxSize(),
                    verticalArrangement = Arrangement.spacedBy(JSpace.m, Alignment.CenterVertically),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Icon(JIcons.brokenImage, null, tint = JTheme.colors.secondaryText, modifier = Modifier.size(JSize.avatar))
                    Text("图片加载失败", style = JTheme.type.secondary, color = JTheme.colors.secondaryText, maxLines = 1)
                    PillButton("重试", { attempt++ }, Modifier.surface(CircleShape, floating = true), kind = ButtonKind.Neutral)
                }
                is Page.Ready -> ZoomableImage(
                    s.bitmap,
                    onZoomed = { zoomed[page] = it },
                    onTap = { chrome = !chrome },
                    onPull = { pull = it },
                    onPullEnd = { far -> if (far) onBack() else pull = 0f },
                )
                Page.Loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = Color.White, strokeWidth = JSize.rule, modifier = Modifier.size(JSize.wellXS))
                }
            }
        }

        AnimatedVisibility(chrome && pull == 0f, enter = fadeIn(), exit = fadeOut(), modifier = Modifier.fillMaxSize()) {
            val shown = ready[pager.currentPage]
            ViewerChrome(
                index = pager.currentPage, count = images.size, saving = saving, ready = shown != null, canEdit = args.canEdit,
                onClose = onBack,
                onSave = { if (MediaShare.needsStoragePermission(ctx)) askStorage.launch(Manifest.permission.WRITE_EXTERNAL_STORAGE) else save() },
                onEdit = { editing = shown },
                onShare = {
                    scope.launch {
                        val img = current()
                        val bytes = ChatImageLoader.bytes(hub, img)
                        if (bytes == null) MediaShare.toast(ctx, "图片还没取到")
                        else runCatching { MediaShare.shareImage(ctx, bytes, img.mime ?: MediaShare.sniffMime(bytes)) }
                            .onFailure { MediaShare.toast(ctx, "分享失败") }
                    }
                },
                onCopy = {
                    scope.launch {
                        val img = current()
                        val bytes = ChatImageLoader.bytes(hub, img)
                        val ok = bytes != null && runCatching { MediaShare.copyImage(ctx, bytes, img.mime ?: MediaShare.sniffMime(bytes)) }.isSuccess
                        MediaShare.toast(ctx, if (ok) "已拷贝图片" else "拷贝失败")
                    }
                },
            )
        }
    }
    }

    editing?.let { img ->
        ImageEditor(img, onCancel = { editing = null }) { edited ->
            onEdited(MediaShare.jpegPayload(edited))
            MediaShare.toast(ctx, "已加到输入框，补一句话就能发")
            editing = null
            onBack()
        }
    }
}

/**
 * The viewer's controls (iOS `ImageViewer.overlay`), on the dark appearance the viewer sets: ✕ and 「n / m」 at the
 * top; 保存, 编辑 ([canEdit]), then 分享 and 拷贝 at the bottom once the picture is [ready]. Everything sits on an
 * opaque floating surface, so it reads over any picture; 44 tall, like the round buttons.
 */
@Composable
internal fun ViewerChrome(
    index: Int, count: Int, saving: Boolean, ready: Boolean, canEdit: Boolean,
    onClose: () -> Unit, onSave: () -> Unit, onEdit: () -> Unit, onShare: () -> Unit, onCopy: () -> Unit,
) {
    Column(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().padding(horizontal = JSpace.l, vertical = JSpace.s)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            CircleButton(JIcons.close, "关闭", onClose)
            Spacer(Modifier.weight(1f))
            if (count > 1) {
                Text(
                    "${index + 1} / $count", style = JTheme.type.secondaryStrong, color = JTheme.colors.label, maxLines = 1,
                    modifier = Modifier.surface(CircleShape, floating = true).padding(horizontal = JSpace.m, vertical = JSpace.s),
                )
            }
        }
        Spacer(Modifier.weight(1f))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(JSpace.m), verticalAlignment = Alignment.CenterVertically) {
            PillButton(
                if (saving) "保存中" else "保存", onSave, Modifier.heightIn(min = JSize.tapTarget),
                kind = ButtonKind.Prominent, icon = JIcons.save, enabled = !saving && ready,
            )
            if (canEdit && ready) {
                PillButton("编辑", onEdit, Modifier.heightIn(min = JSize.tapTarget).surface(CircleShape, floating = true), kind = ButtonKind.Neutral, icon = JIcons.markup)
            }
            Spacer(Modifier.weight(1f))
            if (ready) {
                CircleButton(JIcons.share, "分享", onShare)
                CircleButton(JIcons.copy, "拷贝", onCopy)
            }
        }
    }
}

/**
 * One page: pinch to zoom (1–6×), drag to pan while zoomed, double tap toggles 2.5× at the finger, a tap goes to
 * [onTap]; at 1× a mostly vertical drag pulls the picture away ([onPull]) and far enough closes the viewer.
 * Horizontal drags at 1× are left to the pager.
 */
@Composable
private fun ZoomableImage(
    bitmap: Bitmap,
    onZoomed: (Boolean) -> Unit,
    onTap: () -> Unit,
    onPull: (Float) -> Unit,
    onPullEnd: (Boolean) -> Unit,
) {
    val image = remember(bitmap) { bitmap.asImageBitmap() }
    var size by remember { mutableStateOf(IntSize.Zero) }
    val scale = remember { Animatable(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }
    var pullY by remember { mutableFloatStateOf(0f) }
    val scope = rememberCoroutineScope()
    val slop = LocalViewConfiguration.current.touchSlop
    val closeAt = with(LocalDensity.current) { 120.dp.toPx() }

    fun clamp(o: Offset, s: Float): Offset {
        val maxX = (size.width * (s - 1f)) / 2f
        val maxY = (size.height * (s - 1f)) / 2f
        return Offset(o.x.coerceIn(-maxX, maxX), o.y.coerceIn(-maxY, maxY))
    }

    Box(
        Modifier.fillMaxSize()
            .onSizeChanged { size = it }
            .pointerInput(bitmap) {
                detectTapGestures(
                    onTap = { onTap() },
                    onDoubleTap = { p ->
                        scope.launch {
                            if (scale.value > 1.01f) {
                                offset = Offset.Zero
                                scale.animateTo(1f)
                                onZoomed(false)
                            } else {
                                val target = 2.5f
                                val center = Offset(size.width / 2f, size.height / 2f)
                                offset = clamp((center - p) * (target - 1f), target)
                                onZoomed(true)
                                scale.animateTo(target)
                            }
                        }
                    },
                )
            }
            .pointerInput(bitmap) {
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    var total = Offset.Zero
                    var pulling = false
                    var horizontal = false
                    do {
                        val event = awaitPointerEvent()
                        val pressed = event.changes.count { it.pressed }
                        if (pressed >= 2) {
                            val s = (scale.value * event.calculateZoom()).coerceIn(1f, 6f)
                            scope.launch { scale.snapTo(s) }
                            offset = clamp(offset + event.calculatePan(), s)
                            onZoomed(s > 1.01f)
                            event.changes.forEach { if (it.positionChanged()) it.consume() }
                        } else if (scale.value > 1.01f) {
                            offset = clamp(offset + event.calculatePan(), scale.value)
                            event.changes.forEach { if (it.positionChanged()) it.consume() }
                        } else if (!horizontal) {
                            val pan = event.calculatePan()
                            total += pan
                            if (!pulling) {
                                if (abs(total.y) > slop && abs(total.y) > abs(total.x) * 1.5f) pulling = true
                                else if (abs(total.x) > slop) horizontal = true
                            }
                            if (pulling) {
                                pullY += pan.y
                                onPull(pullY)
                                event.changes.forEach { if (it.positionChanged()) it.consume() }
                            }
                        }
                    } while (event.changes.any { it.pressed })
                    if (pulling) {
                        val far = abs(pullY) > closeAt
                        if (!far) pullY = 0f
                        onPullEnd(far)
                    }
                    if (scale.value <= 1.01f) {
                        offset = Offset.Zero
                        onZoomed(false)
                    }
                }
            },
    ) {
        Image(
            image,
            contentDescription = "图片",
            contentScale = ContentScale.Fit,
            modifier = Modifier.fillMaxSize().graphicsLayer {
                scaleX = scale.value
                scaleY = scale.value
                translationX = offset.x
                translationY = offset.y + pullY
            },
        )
    }
}
