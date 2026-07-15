# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.42"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.42/gestaltd-macos-arm64.tar.gz"
      sha256 "f447de60e71f68e336b60bdeae9c2213b98be1d50c45e04f31dffadc42e20484"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.42/gestaltd-macos-x86_64.tar.gz"
      sha256 "e5d3b7846eda409a4ea31e2987729f05ad51c04e4325821377fb16006f884ab5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.42/gestaltd-linux-arm64.tar.gz"
      sha256 "8e5a056724e95c48e74da0634f83db91458fd4dd1bf14894fd3f57524b4dbd24"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.42/gestaltd-linux-x86_64.tar.gz"
      sha256 "1f65619dcaaa6331d8939259b983ac6bded2c848533145166d8337d1d898ee79"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
