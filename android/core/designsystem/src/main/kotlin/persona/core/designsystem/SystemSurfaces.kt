package persona.core.designsystem

/**
 * The tokens for surfaces the system draws from the app's description, outside Compose's theme: the home-screen widget
 * (Glance) cannot read [PersonaTheme], so it takes the same palette and type scale from here, for the user's style and
 * for each of light and dark (the launcher switches between them with the system).
 */
object SystemSurfaces {
    fun palette(style: AppStyle, dark: Boolean): PersonaPalette = Palettes.of(style, dark)

    fun type(style: AppStyle): PersonaType = Types.of(style)
}
