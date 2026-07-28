# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.50"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.50/gestaltd-macos-arm64.tar.gz"
      sha256 "877953d0d7c4d6c47cd7593d3b5857536ead6ec0f74170a3dabe4ee30c119e1f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.50/gestaltd-macos-x86_64.tar.gz"
      sha256 "fad497656e22d6252d2b347660dc7fca94a15437cfc3639aa250690e8bcae306"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.50/gestaltd-linux-arm64.tar.gz"
      sha256 "bdc2bd944065ea7155bd4248017687e8994853c34394d48c5763b593067f4a17"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.50/gestaltd-linux-x86_64.tar.gz"
      sha256 "b1082e9651e023163a8fd3b25a06465900e22fffda0ec77e31fd2f18d524d936"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
