# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.8"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-macos-arm64.tar.gz"
      sha256 "905edcf50c241676001ce4eb419444a703e35034051ba1c3aaf196c839085f85"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-macos-x86_64.tar.gz"
      sha256 "052022f0ffe616cddb54baa1077b5436d0b47942ef93f6eba65f2a19330f69cd"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-linux-arm64.tar.gz"
      sha256 "c8878657a938a578d108ee832123d8ace7085a54007fca8a900cba030900d7b2"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-linux-x86_64.tar.gz"
      sha256 "d6ee7c19fbbfe545624ae5812fd3df718da53122e47dcc563f50df40ad32e9c2"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
